package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAuditActorTypeRoundTrips(t *testing.T) {
	store := NewAdminStore(setupTestDB(t).DB)
	ctx := t.Context()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	service := model.AdminAuditEvent{
		ID: "service-event", RequestID: "request-service", ActorType: model.AuditActorService,
		ActorID: "principal-one", ActorUsername: "nightly", Action: "service_token.request",
		ResourceType: "service_scope", ResourceID: "audit:read", Outcome: model.AuditOutcomeSuccess,
		DetailJSON: "{}", SessionPrefix: "", CreatedAt: now,
	}
	if err := store.CreateAuditEvent(ctx, &service); err != nil {
		t.Fatalf("create service audit event: %v", err)
	}
	admin := model.AdminAuditEvent{
		ID: "admin-event", RequestID: "request-admin", ActorID: "owner-one",
		ActorUsername: "owner", Action: "session.revoke", ResourceType: "session",
		ResourceID: "session-one", Outcome: model.AuditOutcomeSuccess,
		DetailJSON: "{}", CreatedAt: now.Add(time.Second),
	}
	if err := store.CreateAuditEvent(ctx, &admin); err != nil {
		t.Fatalf("create administrator audit event: %v", err)
	}
	page, err := store.ListAuditEvents(ctx, model.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	byID := map[string]model.AdminAuditEvent{}
	for _, event := range page.Events {
		byID[event.ID] = event
	}
	if byID["service-event"].ActorType != model.AuditActorService {
		t.Fatalf("service actor type = %q", byID["service-event"].ActorType)
	}
	if byID["admin-event"].ActorType != model.AuditActorAdmin {
		t.Fatalf("administrator actor type = %q", byID["admin-event"].ActorType)
	}
}

func TestEnsureAuditActorColumnUpgradesLegacyTable(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy-audit.db"))
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer func() { _ = database.Close() }()
	if _, err := database.Exec(`CREATE TABLE admin_audit_events (
		id TEXT PRIMARY KEY, request_id TEXT NOT NULL, actor_id TEXT, actor_username TEXT NOT NULL,
		action TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL,
		outcome TEXT NOT NULL, detail_json TEXT NOT NULL, ip_address TEXT NOT NULL,
		user_agent TEXT NOT NULL, session_prefix TEXT NOT NULL, created_at DATETIME NOT NULL
	); INSERT INTO admin_audit_events VALUES
		('legacy', 'request', 'owner-one', 'owner', 'session.revoke', 'session', 'session-one',
		 'success', '{}', '192.0.2.10', 'beat-test', 'prefix', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create legacy audit table: %v", err)
	}
	for range 2 {
		if err := ensureAuditActorColumn(t.Context(), database); err != nil {
			t.Fatalf("ensure audit actor column: %v", err)
		}
	}
	var actorType string
	if err := database.QueryRow("SELECT actor_type FROM admin_audit_events WHERE id = 'legacy'").Scan(&actorType); err != nil {
		t.Fatalf("read legacy actor type: %v", err)
	}
	if actorType != model.AuditActorAdmin {
		t.Fatalf("legacy actor type = %q, want admin", actorType)
	}
}
