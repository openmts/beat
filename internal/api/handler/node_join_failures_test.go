package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestJoinedNodeReadsDoNotHideUnavailableMetrics(t *testing.T) {
	sqlite, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqlite.DB)
	node, err := nodes.UpsertNode(t.Context(), "joined-metrics", "192.0.2.10", 22)
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewNodeHandler(nodes, metrics)
	now := time.Now().UTC()
	query := url.Values{"metric": {"cpu"}, "from": {now.Add(-time.Minute).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}
	for _, call := range []http.HandlerFunc{handler.HandleListNodes, handler.HandleGetNode, handler.HandleGetNodeMetrics} {
		request := httptest.NewRequest(http.MethodGet, "/?"+query.Encode(), nil)
		request.SetPathValue("id", node.ID)
		response := httptest.NewRecorder()
		call(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("unavailable metrics returned status %d", response.Code)
		}
		if strings.Contains(response.Body.String(), `"metrics":{}`) {
			t.Fatal("storage failure looked like an empty successful snapshot")
		}
	}
}

func TestJoinedNodeDoesNotAcknowledgeFailedCustomMetricWrite(t *testing.T) {
	sqlite, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqlite.DB)
	node, err := nodes.UpsertNode(t.Context(), "custom-metric-node", "192.0.2.10", 22)
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	bindReportedMetrics(t, sqlite, reportMetricBindings{nodeID: node.ID, names: []string{"collector_health"}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", strings.NewReader(
		testutil.VersionedAgentReport(t, `{"host":"192.0.2.10","port":22,"metrics":{},"custom_metrics":{"collector_health":1}}`)))
	request = request.WithContext(middleware.WithAgentIdentity(request.Context(), testutil.AgentTestIdentity(*node)))
	response := httptest.NewRecorder()
	NewNodeHandler(nodes, metrics).WithMetricCatalog(store.NewMetricCatalogStore(sqlite.DB)).HandleNodeReport(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "storage_unavailable") {
		t.Fatal("failed metric report was acknowledged")
	}
}

func TestJoinEntryBudgetRejectionDoesNotReachStorage(t *testing.T) {
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{})
	handler.issueBudget = newEnrollmentBudget(enrollmentBudgetPolicy{})
	handler.joinBudget = newEnrollmentBudget(enrollmentBudgetPolicy{})
	for _, call := range []http.HandlerFunc{handler.HandleIssue, handler.HandleJoin} {
		response := httptest.NewRecorder()
		call(response, enrollmentHTTPRequest(`{"server_url":"https://beat.example"}`, true))
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
			t.Fatal("entry bypassed admission limit")
		}
	}
}
