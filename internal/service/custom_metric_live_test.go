//go:build linux

package service_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

type liveCustomMetricFixture struct {
	registry        *agent.CustomMetricRegistry
	handler         *handler.CustomMetricHandler
	metrics         *store.CustomMetricStore
	metric          *model.CustomMetric
	hub             *service.AgentHub
	tasks           *store.AgentTaskStore
	nodeID, pidPath string
	connections     atomic.Int64
}

func waitLiveCustomMetric(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("live custom metric state did not settle")
	}
}

func newLiveCustomMetricFixture(t *testing.T) *liveCustomMetricFixture {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	node, err := store.NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "live-custom-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &liveCustomMetricFixture{registry: agent.NewCustomMetricRegistry(), metrics: store.NewCustomMetricStore(sqlite.DB), nodeID: node.ID, pidPath: filepath.Join(t.TempDir(), "script.pid")}
	path := "'" + strings.ReplaceAll(fixture.pidPath, "'", "'\\''") + "'"
	fixture.metric, err = fixture.metrics.Create(t.Context(), model.CustomMetric{Name: "long_running", ShellCommand: "umask 077; printf '%s\n' \"$$\" > " + path + "; exec sleep 30", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.metrics.SetMetricNodes(t.Context(), fixture.metric.ID, []string{node.ID}); err != nil {
		t.Fatal(err)
	}
	hub := service.NewAgentHub()
	fixture.hub = hub
	fixture.tasks = store.NewAgentTaskStore(sqlite.DB)
	operations := service.NewAgentChannelService(fixture.tasks).WithCustomMetrics(fixture.metrics)
	fixture.handler = handler.NewCustomMetricHandler(fixture.metrics, service.NewCustomMetricDispatchService(operations, hub))
	channelHandler := handler.NewAgentChannelHandler(operations, hub)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		fixture.connections.Add(1)
		ctx := middleware.WithAgentIdentity(request.Context(), model.AgentIdentity{NodeID: node.ID, NodeName: node.Name, Mode: model.AgentCredentialActive})
		channelHandler.HandleAgentWebSocket(w, request.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	startLiveCustomMetricAgent(t, fixture, server.URL)
	waitLiveCustomMetric(t, func() bool { return fixture.scriptPID() > 1 })
	return fixture
}

func startLiveCustomMetricAgent(t *testing.T, fixture *liveCustomMetricFixture, url string) {
	t.Helper()
	channel, err := agent.NewChannel(url, "test-agent-token", agent.NewMultiTaskExecutor(nil, fixture.registry))
	if err != nil {
		t.Fatal(err)
	}
	collector := agent.NewCustomMetricCollector(fixture.registry)
	ctx, cancel := context.WithCancel(t.Context())
	channelDone, collectorDone := make(chan error, 1), make(chan error, 1)
	go func() { channelDone <- channel.Run(ctx) }()
	go func() { collectorDone <- collector.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		for _, done := range []chan error{channelDone, collectorDone} {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(time.Second):
				t.Error("agent component did not stop")
			}
		}
		waitLiveCustomMetric(t, func() bool { return !fixture.hub.Online(fixture.nodeID) })
	})
}

func (fixture *liveCustomMetricFixture) scriptPID() int {
	content, err := os.ReadFile(fixture.pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0
	}
	return pid
}

func liveMetricProcessRunning(pid int) bool {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(content), ')')
	return end >= 0 && end+2 < len(content) && content[end+2] != 'Z'
}

func TestLiveCustomMetricRevocationStopsScriptWithoutReconnect(t *testing.T) {
	for _, operation := range []string{"delete", "unbind", "disable"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newLiveCustomMetricFixture(t)
			pid := fixture.scriptPID()
			if !liveMetricProcessRunning(pid) {
				t.Fatal("script was not active before revocation")
			}
			request := httptest.NewRequest(http.MethodDelete, "/", nil)
			request.SetPathValue("id", fixture.metric.ID)
			response := httptest.NewRecorder()
			switch operation {
			case "delete":
				fixture.handler.HandleDelete(response, request)
			case "unbind":
				request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"node_ids":[]}`))
				request.SetPathValue("id", fixture.metric.ID)
				fixture.handler.HandleSetMetricNodes(response, request)
			case "disable":
				request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"long_running","shell_command":"printf 1","interval_seconds":5,"enabled":false}`))
				request.SetPathValue("id", fixture.metric.ID)
				fixture.handler.HandleUpdate(response, request)
			}
			if response.Code != 200 {
				t.Fatalf("revocation status=%d", response.Code)
			}
			waitLiveCustomMetric(t, func() bool { return len(fixture.registry.All()) == 0 && !liveMetricProcessRunning(pid) })
			if fixture.connections.Load() != 1 {
				t.Fatal("revocation needed a reconnect")
			}
		})
	}
}
