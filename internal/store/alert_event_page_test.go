package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func seedAlertEventPage(t *testing.T, eventStore *AlertEventStore, base time.Time) {
	t.Helper()
	events := []*model.AlertEvent{
		{ID: "event-a", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusTriggered,
			TriggeredAt: base.Add(3 * time.Hour), RuleNameSnapshot: "CPU rule", NodeNameSnapshot: "Host one"},
		{ID: "event-b", RuleID: "rule-2", NodeID: "node-2", Status: model.AlertStatusResolved,
			TriggeredAt: base.Add(2 * time.Hour), RuleNameSnapshot: "", NodeNameSnapshot: "Host two"},
		{ID: "event-c", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusAcknowledged,
			TriggeredAt: base.Add(time.Hour), RuleNameSnapshot: "CPU rule", NodeNameSnapshot: ""},
	}
	for _, event := range events {
		if err := eventStore.CreateEvent(context.Background(), event); err != nil {
			t.Fatalf("seed alert event: %v", err)
		}
	}
}

func TestListAlertEventsPageCursorAndSnapshots(t *testing.T) {
	database := setupTestDB(t)
	eventStore := NewAlertEventStore(database.DB)
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	seedAlertEventPage(t, eventStore, base)

	first, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-a" || first.Items[1].ID != "event-b" {
		t.Fatalf("expected newest two events, got %+v", first.Items)
	}
	if first.Items[0].RuleNameSnapshot != "CPU rule" || first.Items[1].NodeNameSnapshot != "Host two" {
		t.Fatalf("expected persisted display snapshots, got %+v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("expected a next cursor")
	}

	second, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-c" || second.NextCursor != "" {
		t.Fatalf("expected the remaining event without a cursor, got %+v", second)
	}
	if second.Items[0].NodeNameSnapshot != "" {
		t.Fatalf("expected an empty snapshot to stay empty, got %q", second.Items[0].NodeNameSnapshot)
	}
}

func TestListAlertEventsPageDuplicateTimestampsAndFilters(t *testing.T) {
	database := setupTestDB(t)
	eventStore := NewAlertEventStore(database.DB)
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	for _, id := range []string{"event-a", "event-b", "event-c"} {
		if err := eventStore.CreateEvent(t.Context(), &model.AlertEvent{
			ID: id, RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusTriggered, TriggeredAt: base,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 3; page++ {
		result, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, item := range result.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate event %q across pages", item.ID)
			}
			seen[item.ID] = true
		}
		cursor = result.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("expected every tied event exactly once, got %v", seen)
	}

	scoped, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{
		Status: string(model.AlertStatusTriggered), RuleID: "rule-1", Limit: 10,
	})
	if err != nil {
		t.Fatalf("filtered page: %v", err)
	}
	if len(scoped.Items) != 3 {
		t.Fatalf("expected the status/rule filter to keep all three, got %d", len(scoped.Items))
	}
	empty, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{NodeID: "node-missing", Limit: 10})
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("expected an empty filtered page, got %+v (%v)", empty, err)
	}
}

func TestListAlertEventsPageRejectsInvalidQuery(t *testing.T) {
	database := setupTestDB(t)
	eventStore := NewAlertEventStore(database.DB)
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	seedAlertEventPage(t, eventStore, base)

	first, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("expected a bound cursor, got %+v (%v)", first, err)
	}
	cases := []struct {
		name  string
		query AlertEventQuery
	}{
		{"limit too large", AlertEventQuery{Limit: AlertEventMaximumPage + 1}},
		{"negative limit", AlertEventQuery{Limit: -1}},
		{"unknown status", AlertEventQuery{Status: "closed"}},
		{"malformed cursor", AlertEventQuery{Cursor: "not-a-cursor"}},
		{"cross-status cursor", AlertEventQuery{Cursor: first.NextCursor, Status: string(model.AlertStatusResolved)}},
		{"cross-rule cursor", AlertEventQuery{Cursor: first.NextCursor, RuleID: "rule-2"}},
		{"cross-time cursor", AlertEventQuery{Cursor: first.NextCursor, To: timePointer(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))}},
	}
	for _, test := range cases {
		if _, err := eventStore.ListAlertEventsPage(t.Context(), test.query); !errors.Is(err, ErrInvalidAlertEventQuery) {
			t.Fatalf("%s: expected ErrInvalidAlertEventQuery, got %v", test.name, err)
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestListAlertEventsPageDefaultAndTimeWindow(t *testing.T) {
	database := setupTestDB(t)
	eventStore := NewAlertEventStore(database.DB)
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	for index := range 60 {
		if err := eventStore.CreateEvent(t.Context(), &model.AlertEvent{
			ID:     "event-" + string(rune('a'+index%26)) + string(rune('0'+index/26)),
			RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusTriggered,
			TriggeredAt: base.Add(time.Duration(index) * time.Minute),
		}); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}
	page, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{})
	if err != nil {
		t.Fatalf("default page: %v", err)
	}
	if len(page.Items) != AlertEventDefaultPage || page.NextCursor == "" {
		t.Fatalf("expected %d items and a cursor, got %d", AlertEventDefaultPage, len(page.Items))
	}

	from, to := base.Add(30*time.Minute), base.Add(45*time.Minute)
	window, err := eventStore.ListAlertEventsPage(t.Context(), AlertEventQuery{From: &from, To: &to, Limit: AlertEventMaximumPage})
	if err != nil {
		t.Fatalf("window page: %v", err)
	}
	if len(window.Items) != 16 {
		t.Fatalf("expected 16 events inside the window, got %d", len(window.Items))
	}
}

func TestCreateActiveEventTxPersistsDisplaySnapshots(t *testing.T) {
	database := setupTestDB(t)
	events := NewAlertEventStore(database.DB)
	event := model.AlertEvent{
		ID: "event-tx", RuleID: "rule-1", NodeID: "node-1", Message: "CPU high",
		Status: model.AlertStatusTriggered, TriggeredAt: time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC),
		RuleName: "CPU rule", NodeName: "Host one",
	}
	transaction := alertTestTx(t, database.DB)
	created, err := events.CreateActiveEventTx(t.Context(), transaction, &event)
	if err != nil || !created {
		t.Fatalf("create active event: created=%v err=%v", created, err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	stored, err := events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored == nil {
		t.Fatalf("read stored event: %v", err)
	}
	if stored.RuleNameSnapshot != "CPU rule" || stored.NodeNameSnapshot != "Host one" {
		t.Fatalf("expected persisted snapshots, got %+v", stored)
	}
}
