package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func governedNodeReportFixture(t *testing.T) (*NodeHandler, *store.SQLiteStore, *model.Node) {
	t.Helper()
	sqlite, mts := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqlite.DB)
	node, err := nodes.UpsertNode(t.Context(), "governed-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	return NewNodeHandler(nodes, mts).WithMetricCatalog(store.NewMetricCatalogStore(sqlite.DB)), sqlite, node
}

func governedNodeReport(t *testing.T, node *model.Node, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", strings.NewReader(testutil.VersionedAgentReport(t, body)))
	return request.WithContext(middleware.WithAgentIdentity(request.Context(), testutil.AgentTestIdentity(*node)))
}

type reportMetricBindings struct {
	nodeID string
	names  []string
}

func bindReportedMetrics(t *testing.T, sqlite *store.SQLiteStore, bindings reportMetricBindings) []model.CustomMetric {
	definitions := make([]model.CustomMetric, 0, len(bindings.names))
	t.Helper()
	metrics := store.NewCustomMetricStore(sqlite.DB)
	for _, name := range bindings.names {
		metric, err := metrics.Create(t.Context(), model.CustomMetric{Name: name, ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, *metric)
		if err := metrics.SetMetricNodes(t.Context(), metric.ID, []string{bindings.nodeID}); err != nil {
			t.Fatal(err)
		}
	}
	return definitions
}

func TestNodeReportRejectsUnboundBatchBeforeHeartbeatOrMTSMutation(t *testing.T) {
	h, _, node := governedNodeReportFixture(t)
	request := governedNodeReport(t, node, `{"host":"192.0.2.99","port":22,"metrics":{"cpu":25},"custom_metrics":{"unknown":7}}`)
	response := httptest.NewRecorder()
	h.HandleNodeReport(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unbound report status=%d", response.Code)
	}
	after, err := h.nodeStore.GetNode(t.Context(), node.ID)
	if err != nil || after.Host != node.Host || !after.LastSeen.Equal(node.LastSeen) {
		t.Fatal("invalid custom batch changed heartbeat")
	}
	values, err := h.mtsStore.QueryLatest(t.Context(), node.ID)
	if err != nil || len(values) != 0 {
		t.Fatal("invalid custom batch wrote builtin samples")
	}
}

func TestNodeReportKnownDisabledCustomCacheDoesNotBlockHeartbeat(t *testing.T) {
	h, sqlite, node := governedNodeReportFixture(t)
	metrics := store.NewCustomMetricStore(sqlite.DB)
	metric, err := metrics.Create(t.Context(), model.CustomMetric{Name: "queue", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, []string{node.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = sqlite.DB.Exec("UPDATE metric_definitions SET enabled=0 WHERE metric_key='custom_queue'")
	if err != nil {
		t.Fatal(err)
	}
	request := governedNodeReport(t, node, `{"host":"192.0.2.99","port":22,"metrics":{"cpu":25},"custom_metrics":{"queue":7}}`)
	response := httptest.NewRecorder()
	h.HandleNodeReport(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("cached disabled report status=%d", response.Code)
	}
	values, err := h.mtsStore.QueryLatest(t.Context(), node.ID)
	if err != nil || values["cpu"] != 25 {
		t.Fatal("disabled custom cache blocked system metrics")
	}
	now := time.Now().UTC()
	custom, err := h.mtsStore.QueryMetrics(t.Context(), []string{"custom_queue"}, now.Add(-time.Hour), now.Add(time.Second), node.ID)
	if err != nil || len(custom["custom_queue"]) != 0 {
		t.Fatal("disabled custom value was persisted")
	}
}
