package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupProbeScenario(t *testing.T) (*CustomMetricHandler, *store.SQLiteStore, *fakeCustomMetricDispatcher, *model.Node, *model.CustomMetric) {
	t.Helper()
	h, metricStore, dispatcher, sqlite := setupCustomMetricHandler(t)
	ctx := context.Background()
	node, err := store.NewNodeStore(sqlite.DB).UpsertNode(ctx, "probe-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metric, err := metricStore.Create(ctx, model.CustomMetric{
		Name: "probe_latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create metric: %v", err)
	}
	if err := metricStore.SetMetricNodes(ctx, metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("bind metric: %v", err)
	}
	return h, sqlite, dispatcher, node, metric
}

func postProbe(h *CustomMetricHandler, metricID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/custom-metrics/"+metricID+"/probe", strings.NewReader(body))
	req.SetPathValue("id", metricID)
	w := httptest.NewRecorder()
	h.HandleProbe(w, req)
	return w
}

func readProbe(h *CustomMetricHandler, metricID, taskID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/custom-metrics/"+metricID+"/probe/"+taskID, nil)
	req.SetPathValue("id", metricID)
	req.SetPathValue("task_id", taskID)
	w := httptest.NewRecorder()
	h.HandleProbeResult(w, req)
	return w
}

func TestCustomMetricProbeHandlerQueuesAndReadsResult(t *testing.T) {
	h, sqlite, dispatcher, node, metric := setupProbeScenario(t)

	w := postProbe(h, metric.ID, `{"node_id":"`+node.ID+`"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("probe status = %d, body = %s", w.Code, w.Body.String())
	}
	queued := decodeData[customMetricProbeResponse](t, w.Body.Bytes())
	if queued.TaskID == "" || queued.NodeID != node.ID || queued.Status != model.AgentTaskStatusPending {
		t.Fatalf("queued = %#v", queued)
	}
	if queued.Success != nil {
		t.Fatalf("pending response carries success = %v", *queued.Success)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.count())
	}

	w = readProbe(h, metric.ID, queued.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("result status = %d, body = %s", w.Code, w.Body.String())
	}
	pending := decodeData[customMetricProbeResponse](t, w.Body.Bytes())
	if pending.Status != model.AgentTaskStatusPending || pending.Success != nil {
		t.Fatalf("pending = %#v", pending)
	}

	tasks := store.NewAgentTaskStore(sqlite.DB)
	if err := tasks.CompleteTask(context.Background(), queued.TaskID, model.AgentTaskStatusDone, "", "0.42"); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	done := decodeData[customMetricProbeResponse](t, readProbe(h, metric.ID, queued.TaskID).Body.Bytes())
	if done.Status != model.AgentTaskStatusDone || done.Success == nil || !*done.Success {
		t.Fatalf("done = %#v", done)
	}
	if done.Output != "0.42" || done.CompletedAt == nil {
		t.Fatalf("done output = %#v", done)
	}
}

func TestCustomMetricProbeHandlerReportsFailureResult(t *testing.T) {
	h, sqlite, _, node, metric := setupProbeScenario(t)
	queued := decodeData[customMetricProbeResponse](t, postProbe(h, metric.ID, `{"node_id":"`+node.ID+`"}`).Body.Bytes())

	tasks := store.NewAgentTaskStore(sqlite.DB)
	if err := tasks.CompleteTask(context.Background(), queued.TaskID, model.AgentTaskStatusFailed, "no output", ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	failed := decodeData[customMetricProbeResponse](t, readProbe(h, metric.ID, queued.TaskID).Body.Bytes())
	if failed.Success == nil || *failed.Success || failed.Failure != "no output" {
		t.Fatalf("failed = %#v", failed)
	}
}

func TestCustomMetricProbeHandlerRejectsBadRequests(t *testing.T) {
	h, _, _, node, metric := setupProbeScenario(t)
	if w := postProbe(h, metric.ID, `{"node_id":`); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d", w.Code)
	}
	if w := postProbe(h, metric.ID, `{"node_id":"missing-node"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unbound node status = %d", w.Code)
	}
	if w := postProbe(h, "missing-metric", `{"node_id":"`+node.ID+`"}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown metric status = %d", w.Code)
	}
	if w := readProbe(h, metric.ID, "missing-task"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown task status = %d", w.Code)
	}
}

func TestCustomMetricProbeHandlerKeepsPendingOnDispatchFailure(t *testing.T) {
	h, _, dispatcher, node, metric := setupProbeScenario(t)
	dispatcher.err = errors.New("dispatcher offline")

	w := postProbe(h, metric.ID, `{"node_id":"`+node.ID+`"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("probe status = %d, body = %s", w.Code, w.Body.String())
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatcher.count())
	}
}
