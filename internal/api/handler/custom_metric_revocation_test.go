package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func customMetricBoundToNode(t *testing.T, h *CustomMetricHandler, sqlite *store.SQLiteStore) (*model.CustomMetric, string) {
	t.Helper()
	node, err := store.NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "revocation-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	metric, err := h.store.Create(t.Context(), model.CustomMetric{Name: "revoked_metric", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetMetricNodes(t.Context(), metric.ID, []string{node.ID}); err != nil {
		t.Fatal(err)
	}
	return metric, node.ID
}

func TestCustomMetricDeleteAndUnbindNotifyPreviousNodes(t *testing.T) {
	for _, operation := range []string{"delete", "unbind"} {
		t.Run(operation, func(t *testing.T) {
			h, _, dispatcher, sqlite := setupCustomMetricHandler(t)
			metric, _ := customMetricBoundToNode(t, h, sqlite)
			request := httptest.NewRequest(http.MethodDelete, "/", nil)
			request.SetPathValue("id", metric.ID)
			response := httptest.NewRecorder()
			if operation == "delete" {
				h.HandleDelete(response, request)
			} else {
				request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"node_ids":[]}`))
				request.SetPathValue("id", metric.ID)
				h.HandleSetMetricNodes(response, request)
			}
			if response.Code != 200 || dispatcher.count() != 1 {
				t.Fatalf("status=%d previous-node notifications=%d", response.Code, dispatcher.count())
			}
		})
	}
}

func TestCustomMetricDeleteRollsBackWhenRevocationCannotBeQueued(t *testing.T) {
	h, _, _, sqlite := setupCustomMetricHandler(t)
	metric, nodeID := customMetricBoundToNode(t, h, sqlite)
	if _, err := sqlite.DB.Exec(`CREATE TRIGGER reject_custom_task BEFORE INSERT ON agent_tasks BEGIN SELECT RAISE(ABORT,'task store failed'); END`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/", nil)
	request.SetPathValue("id", metric.ID)
	response := httptest.NewRecorder()
	h.HandleDelete(response, request)
	if response.Code != 500 {
		t.Fatalf("deletion acknowledged without durable revocation: %d", response.Code)
	}
	remaining, err := h.store.Get(t.Context(), metric.ID)
	if err != nil || remaining == nil {
		t.Fatalf("definition lost after queue failure: %v", err)
	}
	ids, err := h.store.NodeIDsForMetric(t.Context(), metric.ID)
	if err != nil || len(ids) != 1 || ids[0] != nodeID {
		t.Fatal("bindings lost after queue failure")
	}
}
