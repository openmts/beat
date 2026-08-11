package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupMetricsTest(t *testing.T) (*MetricsHandler, *store.MTSStore) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create mts store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	return NewMetricsHandler(
		store.NewNodeStore(sqliteStore.DB),
		mtsStore,
		store.NewSiteSettingsStore(sqliteStore.DB),
	), mtsStore
}

func TestNewMetricsHandler(t *testing.T) {
	h, _ := setupMetricsTest(t)
	if h == nil || h.nodeStore == nil || h.mtsStore == nil {
		t.Fatalf("handler = %#v", h)
	}
}

func TestHandleMetricsWS(t *testing.T) {
	h, _ := setupMetricsTest(t)
	server := httptest.NewServer(http.HandlerFunc(h.HandleMetricsWS))
	t.Cleanup(server.Close)

	conn, err := websocket.Dial("ws"+server.URL[4:], "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var message metricsSnapshot
	if err := websocket.JSON.Receive(conn, &message); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if message.Timestamp == "" || message.Nodes == nil {
		t.Fatalf("snapshot = %#v", message)
	}
}

func TestHandleMetricsWSNoStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(NewMetricsHandler(nil, nil).HandleMetricsWS))
	t.Cleanup(server.Close)
	conn, err := websocket.Dial("ws"+server.URL[4:], "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var message metricsSnapshot
	if err := websocket.JSON.Receive(conn, &message); err != nil {
		t.Fatalf("receive empty snapshot: %v", err)
	}
	if message.Nodes == nil || len(message.Nodes) != 0 {
		t.Fatalf("snapshot = %#v", message)
	}
	_ = conn.Close()
}

func TestMetricsSnapshotIncludesMTSTraffic(t *testing.T) {
	handler, mtsStore := setupMetricsTest(t)
	node, err := handler.nodeStore.UpsertNode(t.Context(), "traffic-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	limit := int64(1000)
	limitType := model.TrafficLimitSum
	resetDay := 1
	if _, err := handler.nodeStore.UpdateNode(t.Context(), node.ID, store.NodeUpdate{
		GroupID: node.GroupID, TrafficLimit: &limit,
		TrafficLimitType: &limitType, TrafficResetDay: &resetDay,
	}); err != nil {
		t.Fatalf("update node traffic policy: %v", err)
	}
	now := time.Now().UTC()
	for index, totals := range []struct{ received, sent float64 }{{100, 200}, {160, 240}} {
		if err := mtsStore.WriteNodeMetrics(t.Context(), store.NodeMetricSample{
			NodeID: node.ID, Metrics: model.NodeMetrics{
				NetRecvTotal: totals.received, NetSentTotal: totals.sent,
			}, Timestamp: now.Add(time.Duration(index-2) * time.Minute),
		}); err != nil {
			t.Fatalf("write traffic sample: %v", err)
		}
	}
	if err := mtsStore.Flush(t.Context()); err != nil {
		t.Fatalf("flush traffic samples: %v", err)
	}
	snapshot, err := handler.snapshot(t.Context())
	if err != nil {
		t.Fatalf("build metrics snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Traffic.Used != 100 {
		t.Fatalf("snapshot traffic = %#v", snapshot.Nodes)
	}
}

func TestMetricsSnapshotExcludesHiddenNodes(t *testing.T) {
	handler, _ := setupMetricsTest(t)
	node, err := handler.nodeStore.UpsertNode(t.Context(), "hidden-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	isPublic := false
	if _, err := handler.nodeStore.UpdateNode(t.Context(), node.ID, store.NodeUpdate{
		Alias: node.Alias, GroupID: node.GroupID, SSHPublicKey: node.SSHPublicKey,
		IsPublic: &isPublic,
	}); err != nil {
		t.Fatalf("hide node: %v", err)
	}
	snapshot, err := handler.snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 0 {
		t.Fatalf("snapshot nodes = %#v", snapshot.Nodes)
	}
}

func TestMetricsSnapshotRedactsPublicHost(t *testing.T) {
	handler, _ := setupMetricsTest(t)
	if _, err := handler.nodeStore.UpsertNode(t.Context(), "public-node", "2001:db8::1", 22); err != nil {
		t.Fatalf("create node: %v", err)
	}
	settings, err := handler.settingsStore.Get(t.Context())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	settings.ShowIPAddresses = false
	if _, err := handler.settingsStore.Update(t.Context(), settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	snapshot, err := handler.snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Host != "" {
		t.Fatalf("snapshot nodes = %#v", snapshot.Nodes)
	}
}
