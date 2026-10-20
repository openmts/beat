package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestRevealSecurityEventSourceUsesLinkedAuditRecord(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)

	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	audit := securityTestAudit(now, "audit-login")
	audit.IPAddress = "203.0.113.47"
	if err := insertAuditEvent(t.Context(), tx, &audit); err != nil {
		t.Fatalf("seed login audit: %v", err)
	}
	event := model.SecurityEvent{ID: "event-source", Kind: model.SecurityEventLoginSucceeded,
		Severity: model.SecuritySeverityInfo, AuthMethod: model.SecurityAuthMethodPassword,
		SourceClass: model.SecuritySourceClassPublic, SourceFamily: model.SecuritySourceFamilyIPv4,
		SourcePrefix: "203.0.113.0/24", BrowserFamily: "chrome", OSFamily: "linux",
		DeviceClass: model.SecurityDeviceClassDesktop, PolicyRevision: 1, SourceKey: "key:event-source",
		SourceKind: SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		AuditEventID: audit.ID, OccurredAt: now, RetainUntil: now.AddDate(0, 0, 180)}
	if err := store.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed security event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	reveal := SecurityEventSourceReveal{EventID: event.ID, ActorID: "owner", At: now.Add(time.Minute),
		Audit: securityTestAudit(now.Add(time.Minute), "audit-reveal")}
	source, err := store.RevealSecurityEventSource(t.Context(), reveal)
	if err != nil || source.Address != "203.0.113.47" || source.EventID != event.ID {
		t.Fatalf("revealed source = %+v / %v", source, err)
	}
	var audited int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE id='audit-reveal'`).Scan(&audited); err != nil {
		t.Fatalf("count reveal audit: %v", err)
	}
	if audited != 1 {
		t.Fatalf("reveal audit rows = %d, want 1", audited)
	}
	if _, err := store.RevealSecurityEventSource(t.Context(), reveal); err == nil {
		t.Fatal("repeated reveal with the same audit id must fail")
	}
}

func TestRevealSecurityEventSourceRejectsUnlinkedAndIncompleteRequests(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	seedSecurityEvent(t, store, "event-unlinked", model.SecurityEventLoginSuspicious, model.SecuritySeverityHigh,
		model.SecurityReasonRateLimitLockout, model.SecuritySourceClassPublic, "203.0.113.0/24", now)

	if _, err := store.RevealSecurityEventSource(t.Context(), SecurityEventSourceReveal{EventID: "event-unlinked",
		ActorID: "owner", At: now, Audit: securityTestAudit(now, "audit-unlinked")}); !errors.Is(err, ErrSecurityEventSourceUnavailable) {
		t.Fatalf("unlinked event reveal = %v", err)
	}
	if _, err := store.RevealSecurityEventSource(t.Context(), SecurityEventSourceReveal{}); !errors.Is(err, ErrSecurityEventSourceUnavailable) {
		t.Fatalf("incomplete reveal = %v", err)
	}
	if _, err := store.RevealSecurityEventSource(t.Context(), SecurityEventSourceReveal{EventID: "missing",
		ActorID: "owner", At: now, Audit: securityTestAudit(now, "audit-missing")}); !errors.Is(err, ErrSecurityEventNotFound) {
		t.Fatalf("missing event reveal = %v", err)
	}
	seedBlankAddressEvent(t, store, database, now)
	if _, err := store.RevealSecurityEventSource(t.Context(), SecurityEventSourceReveal{EventID: "event-blank-address",
		ActorID: "owner", At: now, Audit: securityTestAudit(now, "audit-blank-address")}); !errors.Is(err, ErrSecurityEventSourceUnavailable) {
		t.Fatalf("blank address reveal = %v", err)
	}
}

// seedBlankAddressEvent 写入一条审计来源地址为空的事件，用于验证空地址拒绝路径。
func seedBlankAddressEvent(t *testing.T, store *AdminStore, database *SQLiteStore, now time.Time) {
	t.Helper()
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	audit := securityTestAudit(now, "audit-blank-source")
	audit.IPAddress = ""
	if err := insertAuditEvent(t.Context(), tx, &audit); err != nil {
		t.Fatalf("seed blank audit: %v", err)
	}
	event := model.SecurityEvent{ID: "event-blank-address", Kind: model.SecurityEventLoginSucceeded,
		Severity: model.SecuritySeverityInfo, AuthMethod: model.SecurityAuthMethodPassword,
		SourceClass: model.SecuritySourceClassPublic, SourceFamily: model.SecuritySourceFamilyIPv4,
		SourcePrefix: "203.0.113.0/24", BrowserFamily: "chrome", OSFamily: "linux",
		DeviceClass: model.SecurityDeviceClassDesktop, PolicyRevision: 1, SourceKey: "key:event-blank-address",
		SourceKind: SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		AuditEventID: audit.ID, OccurredAt: now, RetainUntil: now.AddDate(0, 0, 180)}
	if err := store.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed blank address event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
