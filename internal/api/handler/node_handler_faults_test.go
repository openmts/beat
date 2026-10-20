package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

func closedStoreDatabase(t *testing.T) *store.SQLiteStore {
	t.Helper()
	database, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	return database
}

// TestNodeHandlerOptionalDependencyWiring 覆盖节点处理器可选依赖的注入链。
func TestNodeHandlerOptionalDependencyWiring(t *testing.T) {
	handler := NewNodeHandler(nil, nil)
	if handler.WithAgentHub(&service.AgentHub{}) != handler {
		t.Fatal("agent hub injection returned a different handler")
	}
	if handler.WithTrafficResolver(nil) != handler {
		t.Fatal("traffic resolver injection returned a different handler")
	}
	if handler.WithMetricQueryBudget(nil) != handler {
		t.Fatal("metric query budget injection returned a different handler")
	}
	if handler.agentHub == nil {
		t.Fatal("agent hub was not retained")
	}
}

// TestHandleGetNodeCustomMetricsFaults 覆盖自定义指标查询在节点库、指标库与目录三层依赖失败时的响应。
func TestHandleGetNodeCustomMetricsFaults(t *testing.T) {
	closed := closedStoreDatabase(t)
	handler := NewNodeHandler(store.NewNodeStore(closed.DB), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/id/custom-metrics", nil)
	request.SetPathValue("id", "id")
	w := httptest.NewRecorder()
	handler.HandleGetNodeCustomMetrics(w, request)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unreadable node status = %d", w.Code)
	}

	database, mtsStore := setupNodeTestDB(t)
	ctx := context.Background()
	nodeStore := store.NewNodeStore(database.DB)
	node, err := nodeStore.UpsertNode(ctx, "fault-mode-node", "10.0.0.11", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	handler = NewNodeHandler(nodeStore, mtsStore).WithCustomMetrics(store.NewCustomMetricStore(closed.DB))
	w = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/custom-metrics", nil)
	request.SetPathValue("id", node.ID)
	handler.HandleGetNodeCustomMetrics(w, request)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unreadable custom metrics status = %d", w.Code)
	}

	handler = NewNodeHandler(nodeStore, mtsStore).WithCustomMetrics(store.NewCustomMetricStore(database.DB)).
		WithMetricCatalog(store.NewMetricCatalogStore(closed.DB))
	w = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/custom-metrics", nil)
	request.SetPathValue("id", node.ID)
	handler.HandleGetNodeCustomMetrics(w, request)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unreadable metric catalog status = %d", w.Code)
	}
}
