package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/metricsfanout"
)

func TestRuntimeSharesPublicProducerAndRemovesLegacyStreams(t *testing.T) {
	fixture := newCommercialFixture(t)
	trusted, err := middleware.ParseTrustedProxies("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	state := newRuntimeState()
	handler, _, stream, err := buildHTTPHandler(httpHandlerConfig{dataDir: fixture.directory, staticDir: t.TempDir(), stores: fixture.stores, services: fixture.services, trustedProxies: trusted, runtime: state})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	state.workers.bind(ctx, cancel)
	tasks := &backgroundTasks{runtime: state, cancel: cancel, fanout: stream}
	startFanoutWorker(backgroundConfig{ctx: ctx, runtime: state, fanout: stream}, tasks)
	t.Cleanup(tasks.stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	connections := []*websocket.Conn{}
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	var payload string
	for index := range 12 {
		config, err := websocket.NewConfig("ws"+server.URL[4:]+"/api/v1/ws/metrics", server.URL)
		if err != nil {
			t.Fatal(err)
		}
		config.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", index+1))
		connection, err := websocket.DialConfig(config)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := websocket.Message.Receive(connection, &raw); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			payload = raw
		} else if raw != payload {
			t.Fatal("connections received different generation")
		}
	}
	if stats := stream.Stats(); stats.Builds != 1 || stats.Connections != 12 || stats.TrackedIPs != 12 {
		t.Fatal(stats)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &shape); err != nil || len(shape) != 2 || shape["timestamp"] == nil || shape["nodes"] == nil {
		t.Fatal(payload, err)
	}
	for _, path := range []string{"/ws", "/metrics/ws"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 404 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, response.Code)
		}
	}
	// 读取server close并退出reader，验证关闭会等待全部被劫持连接。
	var readers sync.WaitGroup
	for _, connection := range connections {
		readers.Go(func() { var value string; _ = websocket.Message.Receive(connection, &value); _ = connection.Close() })
	}
	tasks.stop()
	readers.Wait()
	if stats := stream.Stats(); stats.Running || !stats.Closed || stats.Reserved != 0 || stats.Connections != 0 {
		t.Fatal(stats)
	}
	if state.workers.terminal.Load() {
		t.Fatal("planned stream close caused fatal worker exit")
	}
	fixture.stores.closeIgnoringErrors()
	before := stream.Stats().Builds
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/ws/metrics", nil))
	if response.Code != 503 || stream.Stats().Builds != before {
		t.Fatal("closed stream admitted work", response.Code)
	}
}

func TestRuntimeFanoutStatusAndReadinessExposeStaleFailure(t *testing.T) {
	state := newRuntimeStateWithWorkers("alerts", "traffic_reports", "maintenance", "metrics_fanout")
	var clockMu sync.Mutex
	now := time.Now().UTC()
	clock := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	stream, err := metricsfanout.New(metricsfanout.Config{Now: clock, Build: func(context.Context) (metricsfanout.Snapshot, error) {
		return metricsfanout.Snapshot{Timestamp: clock(), Value: map[string]any{"timestamp": clock(), "nodes": []any{}}}, nil
	}, ClientIP: middleware.ClientIP, AllowOrigin: func(*http.Request) bool { return true }, Observer: state.observeFanout})
	if err != nil {
		t.Fatal(err)
	}
	state.fanout.Store(stream)
	done := make(chan error, 1)
	go func() { done <- stream.Run(t.Context()) }()
	t.Cleanup(func() {
		_ = stream.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	awaitCommercial(t, func() bool { return stream.Stats().Builds == 1 })
	clockMu.Lock()
	now = now.Add(metricsfanout.StaleWindow + time.Second)
	clockMu.Unlock()
	operations := newTestOperability()
	operations.runtime = state
	response, ready := operations.readiness(t.Context())
	if ready || response.Checks["worker_metrics_fanout"].Detail != "stale" {
		t.Fatal(response, ready)
	}
	state.observeFanout(metricsfanout.Event{Kind: metricsfanout.EventBuild, Reason: metricsfanout.ReasonBuilder, Count: 1})
	state.observeFanout(metricsfanout.Event{Kind: metricsfanout.EventDrop, Reason: metricsfanout.ReasonCoalesced, Count: 2})
	state.observeFanout(metricsfanout.Event{Kind: metricsfanout.EventDrop, Reason: metricsfanout.ReasonStale, Count: 1})
	state.observeFanout(metricsfanout.Event{Kind: metricsfanout.EventUpgrade, Reason: metricsfanout.ReasonOrigin, Count: 1})
	var metrics strings.Builder
	state.writeFanoutMetrics(&metrics)
	for _, name := range []string{"beat_metrics_snapshot_age_seconds", `result="rejected"`, `reason="coalesced"} 2`} {
		if !strings.Contains(metrics.String(), name) {
			t.Fatal(metrics.String())
		}
	}
	body := httptest.NewRecorder()
	state.handleStatus(body, httptest.NewRequest("GET", "/", nil))
	if body.Code != 200 || body.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body.Body.String(), `"server_time"`) || !strings.Contains(body.Body.String(), `"stale":true`) {
		t.Fatal(body.Body.String())
	}
}
