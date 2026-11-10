package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestNetworkHandlerAcknowledgesExpiredBatchWithoutStoring(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)

	postInvalidNetworkResult(t, fixture, created.ID, fixture.now.Add(-10*time.Minute), http.StatusNoContent)
	latest, err := fixture.mts.QueryNetworkLatest(t.Context(), created.ID, fixture.node.ID)
	if err != nil {
		t.Fatalf("query latest: %v", err)
	}
	if latest != nil {
		t.Fatalf("expired result was stored: %#v", latest)
	}
}

func TestNetworkHandlerKeepsFreshResultsInMixedBatch(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)
	targetID := created.Targets[0].ID
	body, err := json.Marshal(map[string]any{
		"node_name": "handler-node",
		"results": []map[string]any{
			expiredNetworkResult(created.ID, targetID, fixture.now.Add(-10*time.Minute)),
			expiredNetworkResult(created.ID, targetID, fixture.now),
		},
	})
	if err != nil {
		t.Fatalf("encode network results: %v", err)
	}
	assertHandlerStatus(t, fixture.handler.HandleNetworkResults,
		networkAgentRequest(fixture, http.MethodPost, "/results", string(body)), http.StatusNoContent)

	latest, err := fixture.mts.QueryNetworkLatest(t.Context(), created.ID, fixture.node.ID)
	if err != nil {
		t.Fatalf("query latest: %v", err)
	}
	if latest == nil || !latest.Timestamp.Equal(fixture.now) {
		t.Fatalf("latest = %#v, want timestamp %v", latest, fixture.now)
	}
}

func expiredNetworkResult(taskID, targetID string, finished time.Time) map[string]any {
	return map[string]any{
		"task_id": taskID, "target_id": targetID, "finished_at": finished.Format(time.RFC3339Nano),
		"latency_ms": 1.5, "success": true, "status_code": 0, "error_code": "none",
	}
}
