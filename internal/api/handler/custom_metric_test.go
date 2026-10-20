package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeCustomMetricDispatcher struct {
	mu        sync.Mutex
	registers []string
	err       error
}

func (f *fakeCustomMetricDispatcher) DispatchCustomMetricTasks(_ context.Context, tasks []model.AgentTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range tasks {
		f.registers = append(f.registers, task.NodeID)
	}
	return f.err
}

func (f *fakeCustomMetricDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registers)
}

func setupCustomMetricHandler(t *testing.T) (*CustomMetricHandler, *store.CustomMetricStore, *fakeCustomMetricDispatcher, *store.SQLiteStore) {
	t.Helper()
	s, _ := setupNodeTestDB(t)
	metricStore := store.NewCustomMetricStore(s.DB)
	dispatcher := &fakeCustomMetricDispatcher{}
	return NewCustomMetricHandler(metricStore, dispatcher), metricStore, dispatcher, s
}

func decodeData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var envelope Response
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return value
}

func TestCustomMetricHandlerCRUD(t *testing.T) {
	h, _, _, _ := setupCustomMetricHandler(t)

	// Create
	body := `{"name":"latency","shell_command":"echo 5","interval_seconds":15,"description":"latency ms"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/custom-metrics", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	created := decodeData[model.CustomMetric](t, w.Body.Bytes())
	if !created.Enabled || created.ID == "" {
		t.Fatalf("created = %#v", created)
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/api/v1/custom-metrics", nil)
	w = httptest.NewRecorder()
	h.HandleList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	list := decodeData[[]model.CustomMetric](t, w.Body.Bytes())
	if len(list) != 1 {
		t.Fatalf("list = %#v", list)
	}

	// Update
	updateBody := `{"name":"latency","shell_command":"echo 10","interval_seconds":60,"description":"","enabled":false}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/custom-metrics/"+created.ID, strings.NewReader(updateBody))
	req.SetPathValue("id", created.ID)
	w = httptest.NewRecorder()
	h.HandleUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", w.Code, w.Body.String())
	}
	updated := decodeData[model.CustomMetric](t, w.Body.Bytes())
	if updated.IntervalSeconds != 60 || updated.Enabled {
		t.Fatalf("updated = %#v", updated)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/custom-metrics/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	w = httptest.NewRecorder()
	h.HandleDelete(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d", w.Code)
	}
}

func TestCustomMetricHandlerValidationAndConflicts(t *testing.T) {
	h, _, _, _ := setupCustomMetricHandler(t)

	// Invalid body
	req := httptest.NewRequest(http.MethodPost, "/api/v1/custom-metrics", strings.NewReader(`{"name":"","shell_command":"echo 1","interval_seconds":2}`))
	w := httptest.NewRecorder()
	h.HandleCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d", w.Code)
	}

	// Create then duplicate name
	body := `{"name":"dup","shell_command":"echo 1","interval_seconds":10}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/custom-metrics", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.HandleCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/custom-metrics", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.HandleCreate(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", w.Code)
	}
}

func TestCustomMetricHandlerNodeBindings(t *testing.T) {
	h, metricStore, dispatcher, s := setupCustomMetricHandler(t)
	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(context.Background(), "custom-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metric, err := metricStore.Create(context.Background(), model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create metric: %v", err)
	}

	// Set nodes for the metric
	body := `{"node_ids":["` + node.ID + `"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/custom-metrics/"+metric.ID+"/nodes", strings.NewReader(body))
	req.SetPathValue("id", metric.ID)
	w := httptest.NewRecorder()
	h.HandleSetMetricNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set metric nodes status = %d, body = %s", w.Code, w.Body.String())
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.count())
	}

	// List nodes for the metric
	req = httptest.NewRequest(http.MethodGet, "/api/v1/custom-metrics/"+metric.ID+"/nodes", nil)
	req.SetPathValue("id", metric.ID)
	w = httptest.NewRecorder()
	h.HandleListMetricNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list metric nodes status = %d", w.Code)
	}
	ids := decodeData[[]string](t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != node.ID {
		t.Fatalf("ids = %v", ids)
	}
}

func TestCustomMetricHandlerUpdateMissing(t *testing.T) {
	h, _, _, _ := setupCustomMetricHandler(t)
	body := `{"name":"x","shell_command":"echo 1","interval_seconds":10}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/custom-metrics/missing", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdate(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing status = %d", w.Code)
	}
}

func TestCustomMetricHandlerDispatchOnUpdate(t *testing.T) {
	h, metricStore, dispatcher, s := setupCustomMetricHandler(t)
	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(context.Background(), "dispatch-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metric, err := metricStore.Create(context.Background(), model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create metric: %v", err)
	}
	if err := metricStore.SetMetricNodes(context.Background(), metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("set metric nodes: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/custom-metrics/"+metric.ID, strings.NewReader(
		`{"name":"latency","shell_command":"echo 9","interval_seconds":30,"description":"","enabled":true}`,
	))
	req.SetPathValue("id", metric.ID)
	w := httptest.NewRecorder()
	h.HandleUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", w.Code, w.Body.String())
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.count())
	}
}

func TestCustomMetricHandlerDispatchNodeError(t *testing.T) {
	h, metricStore, dispatcher, s := setupCustomMetricHandler(t)
	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(context.Background(), "err-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metric, err := metricStore.Create(context.Background(), model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create metric: %v", err)
	}
	if err := metricStore.SetMetricNodes(context.Background(), metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("set metric nodes: %v", err)
	}
	dispatcher.err = errors.New("register failed")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/custom-metrics/"+metric.ID, strings.NewReader(
		`{"name":"latency","shell_command":"echo 9","interval_seconds":30,"description":"","enabled":true}`,
	))
	req.SetPathValue("id", metric.ID)
	w := httptest.NewRecorder()
	h.HandleUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d", w.Code)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.count())
	}
}
