package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// envelope 还原 JSONResponse 的 {code,message,data} 包装。
type envelope[T any] struct {
	Data T `json:"data"`
}

type alertEventPageResponse struct {
	Items []struct {
		ID               string `json:"id"`
		RuleNameSnapshot string `json:"rule_name_snapshot"`
		NodeNameSnapshot string `json:"node_name_snapshot"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func alertEventListFixture(t *testing.T) *AlertHandler {
	t.Helper()
	database := setupTestDB(t)
	eventStore := store.NewAlertEventStore(database.DB)
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	seeds := []*model.AlertEvent{
		{ID: "event-a", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusTriggered,
			TriggeredAt: base.Add(2 * time.Hour), RuleNameSnapshot: "CPU rule", NodeNameSnapshot: "Host one"},
		{ID: "event-b", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusResolved,
			TriggeredAt: base.Add(time.Hour), RuleNameSnapshot: "", NodeNameSnapshot: "Host one"},
	}
	for _, seed := range seeds {
		if err := eventStore.CreateEvent(t.Context(), seed); err != nil {
			t.Fatalf("seed alert event: %v", err)
		}
	}
	return NewAlertHandler(store.NewAlertRuleStore(database.DB), store.NewAlertChannelStore(database.DB),
		eventStore, nil)
}

func serveAlertEvents(t *testing.T, handler *AlertHandler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(t.Context())
	recorder := httptest.NewRecorder()
	handler.HandleListAlertEvents(recorder, request)
	return recorder
}

func TestHandleListAlertEventsPaginatesAndKeepsSnapshots(t *testing.T) {
	handler := alertEventListFixture(t)

	first := serveAlertEvents(t, handler, "/api/v1/alerts/events?limit=1")
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", first.Code)
	}
	var page envelope[alertEventPageResponse]
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Data.Items) != 1 || page.Data.Items[0].ID != "event-a" || page.Data.NextCursor == "" {
		t.Fatalf("expected the first page plus a cursor, got %+v", page)
	}
	if page.Data.Items[0].RuleNameSnapshot != "CPU rule" || page.Data.Items[0].NodeNameSnapshot != "Host one" {
		t.Fatalf("expected persisted snapshots, got %+v", page.Data.Items[0])
	}

	second := serveAlertEvents(t, handler, "/api/v1/alerts/events?limit=1&cursor="+page.Data.NextCursor)
	if second.Code != http.StatusOK {
		t.Fatalf("expected 200 for the cursor page, got %d", second.Code)
	}
	var next envelope[alertEventPageResponse]
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatalf("decode next page: %v", err)
	}
	if len(next.Data.Items) != 1 || next.Data.Items[0].ID != "event-b" || next.Data.NextCursor != "" {
		t.Fatalf("expected the final page without a cursor, got %+v", next)
	}
	if next.Data.Items[0].RuleNameSnapshot != "" {
		t.Fatalf("expected the missing snapshot to stay empty, got %q", next.Data.Items[0].RuleNameSnapshot)
	}
}

func TestHandleListAlertEventsRejectsInvalidQueries(t *testing.T) {
	handler := alertEventListFixture(t)
	for _, target := range []string{
		"/api/v1/alerts/events?limit=0",
		"/api/v1/alerts/events?limit=201",
		"/api/v1/alerts/events?limit=abc",
		"/api/v1/alerts/events?status=closed",
		"/api/v1/alerts/events?cursor=!!!",
		"/api/v1/alerts/events?from=not-a-time",
		"/api/v1/alerts/events?from=2026-10-16T00:00:00Z&to=2026-10-15T00:00:00Z",
	} {
		if recorder := serveAlertEvents(t, handler, target); recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", target, recorder.Code)
		}
	}
	for _, target := range []string{
		"/api/v1/alerts/events",
		"/api/v1/alerts/events?status=resolved",
		"/api/v1/alerts/events?rule_id=rule-1&node_id=node-1",
		"/api/v1/alerts/events?from=2026-10-15T00:00:00Z&to=2026-10-16T00:00:00Z",
	} {
		if recorder := serveAlertEvents(t, handler, target); recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", target, recorder.Code)
		}
	}
}
