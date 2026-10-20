package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func seedAuditEventPage(t *testing.T, store *AdminStore, now time.Time) {
	t.Helper()
	for index, id := range []string{"audit-a", "audit-b", "audit-c"} {
		event := model.AdminAuditEvent{
			ID: id, Action: "node.update", ActorID: "owner", ActorUsername: "owner",
			ResourceType: "node", ResourceID: "node-" + id, Outcome: model.AuditOutcomeSuccess,
			DetailJSON: "{}", CreatedAt: now.Add(time.Duration(index) * time.Minute),
		}
		if err := store.CreateAuditEvent(t.Context(), &event); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func TestListAuditEventsCursorWalksEveryEventOnce(t *testing.T) {
	store := newAdminTestStore(t)
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	seedAuditEventPage(t, store, now)

	first, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Events) != 2 || first.Events[0].ID != "audit-c" || first.NextCursor == "" {
		t.Fatalf("expected the newest two events plus a cursor, got %+v", first)
	}

	second, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != "audit-a" || second.NextCursor != "" {
		t.Fatalf("expected the final page without a cursor, got %+v", second)
	}
}

func TestListAuditEventsKeepsTiesAndFilters(t *testing.T) {
	store := newAdminTestStore(t)
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	for _, id := range []string{"tie-a", "tie-b", "tie-c"} {
		if err := store.CreateAuditEvent(t.Context(), &model.AdminAuditEvent{
			ID: id, Action: "node.create", ActorID: "owner", ActorUsername: "owner",
			ResourceType: "node", ResourceID: id, Outcome: model.AuditOutcomeSuccess,
			DetailJSON: "{}", CreatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seen, cursor := map[string]bool{}, ""
	for page := range 3 {
		next, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, event := range next.Events {
			if seen[event.ID] {
				t.Fatalf("duplicate audit event %q across pages", event.ID)
			}
			seen[event.ID] = true
		}
		cursor = next.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("expected every tied event exactly once, got %v", seen)
	}

	scoped, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Action: "node.create", ActorID: "owner"})
	if err != nil || len(scoped.Events) != 3 {
		t.Fatalf("filtered page = %+v / %v", scoped, err)
	}
	empty, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Action: "node.delete"})
	if err != nil || len(empty.Events) != 0 {
		t.Fatalf("expected an empty filtered page, got %+v / %v", empty, err)
	}
}

func TestListAuditEventsRejectsInvalidCursorAndLimit(t *testing.T) {
	store := newAdminTestStore(t)
	seedAuditEventPage(t, store, time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC))

	bound, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Action: "node.update", Limit: 1})
	if err != nil || bound.NextCursor == "" {
		t.Fatalf("expected a bound cursor, got %+v / %v", bound, err)
	}
	for _, test := range []struct {
		name   string
		filter model.AuditFilter
	}{
		{"limit above maximum", model.AuditFilter{Limit: AuditMaximumPage + 1}},
		{"negative limit", model.AuditFilter{Limit: -1}},
		{"malformed cursor", model.AuditFilter{Cursor: "!!!"}},
		{"oversized cursor", model.AuditFilter{Cursor: strings.Repeat("a", 600)}},
		{"cross-action cursor", model.AuditFilter{Cursor: bound.NextCursor, Action: "node.delete"}},
		{"cross-actor cursor", model.AuditFilter{Cursor: bound.NextCursor, Action: "node.update", ActorID: "other"}},
	} {
		if _, err := store.ListAuditEvents(t.Context(), test.filter); !errors.Is(err, ErrInvalidAuditEventQuery) {
			t.Fatalf("%s: expected ErrInvalidAuditEventQuery, got %v", test.name, err)
		}
	}
}

func TestListAuditEventsDefaultPageSize(t *testing.T) {
	store := newAdminTestStore(t)
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	for index := range 60 {
		if err := store.CreateAuditEvent(t.Context(), &model.AdminAuditEvent{
			ID:     "audit-" + string(rune('a'+index%26)) + string(rune('0'+index/26)),
			Action: "node.update", ActorID: "owner", ActorUsername: "owner",
			ResourceType: "node", ResourceID: "node", Outcome: model.AuditOutcomeSuccess,
			DetailJSON: "{}", CreatedAt: now.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}
	page, err := store.ListAuditEvents(t.Context(), model.AuditFilter{})
	if err != nil {
		t.Fatalf("default page: %v", err)
	}
	if len(page.Events) != AuditDefaultPage || page.NextCursor == "" {
		t.Fatalf("expected %d events and a cursor, got %d", AuditDefaultPage, len(page.Events))
	}
}
