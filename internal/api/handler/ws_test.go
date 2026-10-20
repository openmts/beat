package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupMetricsTest(t *testing.T) (*MetricsHandler, *store.MTSStore) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(handlerFixturePath(t))
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
	startMetricsStream(t, h)
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
	h := NewMetricsHandler(nil, nil)
	if _, err := h.BuildMetricsSnapshot(t.Context()); err == nil {
		t.Fatal("missing stores emitted empty snapshot")
	}
	response := httptest.NewRecorder()
	h.HandleMetricsWS(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != 503 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Header())
	}
}

func startMetricsStream(t *testing.T, h *MetricsHandler) *metricsfanout.Service {
	t.Helper()
	stream, err := metricsfanout.New(metricsfanout.Config{Build: h.BuildMetricsSnapshot, ClientIP: middleware.ClientIP, AllowOrigin: MetricsOriginPolicy(wildcardTestPolicy(t))})
	if err != nil {
		t.Fatal(err)
	}
	h.WithMetricsStream(stream)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- stream.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := stream.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return stream
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
	settings.PublicAddressMode = model.PublicAddressHidden
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

// wildcardTestPolicy 在测试里保留“允许任意来源”的历史装配。
func wildcardTestPolicy(t *testing.T) deploymentaccess.OriginPolicy {
	t.Helper()
	policy, err := deploymentaccess.ParseOrigins("*")
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

// unconfiguredTestPolicy 复用生产路径构造「未配置来源列表」的策略。
func unconfiguredTestPolicy(t *testing.T) deploymentaccess.OriginPolicy {
	t.Helper()
	config, err := deploymentaccess.Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return config.WSOrigins()
}

func TestPublicMetricsOriginFollowsTheDeploymentPolicy(t *testing.T) {
	configured, err := deploymentaccess.ParseOrigins("https://console.example.com")
	if err != nil {
		t.Fatal(err)
	}
	sameOrigin := unconfiguredTestPolicy(t)
	for _, entry := range []struct {
		origin  string
		policy  deploymentaccess.OriginPolicy
		allowed bool
	}{
		{"", wildcardTestPolicy(t), true},
		{"https://other.example.test", wildcardTestPolicy(t), true},
		{"http://%zz", wildcardTestPolicy(t), false},
		{"https://console.example.com", configured, true},
		{"https://evil.example.com", configured, false},
		{"", configured, true},
		{"http://example.test", configured, true},
		{"http://example.test:80", configured, true},
		{"https://EXAMPLE.test:443", configured, true},
		{"http://example.test", sameOrigin, true},
		{"http://EXAMPLE.TEST", sameOrigin, true},
		{"http://other.example.test", sameOrigin, false},
		{"https://evil.example.com", sameOrigin, false},
		{"", sameOrigin, true},
	} {
		request := httptest.NewRequest("GET", "http://example.test/api/v1/ws/metrics", nil)
		request.Header.Set("Origin", entry.origin)
		if MetricsOriginPolicy(entry.policy)(request) != entry.allowed {
			t.Fatal(entry)
		}
	}
}

func TestMetricsBuilderDoesNotTurnClosedMTSIntoAnEmptyFleet(t *testing.T) {
	handler, mts := setupMetricsTest(t)
	value, err := handler.BuildMetricsSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload := value.Value.(metricsSnapshot)
	if value.Timestamp.Format(time.RFC3339) != payload.Timestamp {
		t.Fatal("snapshot timestamp changed")
	}
	if err := mts.Close(); err != nil {
		t.Fatal(err)
	}
	if value, err := handler.BuildMetricsSnapshot(t.Context()); err == nil || value.Value != nil {
		t.Fatal("closed MTS emitted successful payload", value, err)
	}
}
