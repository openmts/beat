package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func applicationRetentionFixture(t *testing.T) (*SQLiteStore, *ApplicationRetentionStore) {
	t.Helper()
	database := setupTestDB(t)
	return database, NewApplicationRetentionStore(database.DB)
}

func TestApplicationRetentionSettingsDefaultsAndValidation(t *testing.T) {
	_, store := applicationRetentionFixture(t)
	settings, status, err := store.Get(t.Context())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.AlertEventDays != 180 || settings.AuditEventDays != 180 ||
		settings.CleanupHourUTC != 3 || !settings.AutoCleanupEnabled {
		t.Fatalf("default settings = %#v", settings)
	}
	if status.Running || status.LastStatus != model.ApplicationRetentionStatusNever {
		t.Fatalf("default status = %#v", status)
	}

	settings.AlertEventDays = 30
	settings.AuditEventDays = 90
	settings.CleanupHourUTC = 5
	updated, err := store.Update(t.Context(), settings)
	if err != nil || updated.AlertEventDays != 30 || updated.UpdatedAt.IsZero() {
		t.Fatalf("updated settings = %#v, error = %v", updated, err)
	}
	if _, err := store.Update(t.Context(), model.ApplicationRetentionSettings{AlertEventDays: 0, AuditEventDays: 90}); err == nil {
		t.Fatal("out-of-range alert retention must be rejected")
	}
	if err := store.MarkStarted(t.Context(), model.ApplicationRetentionTriggerAutomatic, time.Now().UTC()); err != nil {
		t.Fatalf("mark started: %v", err)
	}
	if err := store.MarkCompleted(t.Context(), time.Now().UTC(), 3*time.Second,
		model.ApplicationRetentionStatusSuccess, "", model.ApplicationRetentionCounts{AlertEvents: 2, AuditEvents: 1, Sessions: 4}); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	_, status, err = store.Get(t.Context())
	if err != nil || status.Running || status.LastStatus != model.ApplicationRetentionStatusSuccess ||
		status.DeletedAlerts != 2 || status.DeletedAudits != 1 || status.DeletedSessions != 4 ||
		status.LastDurationMS != 3000 {
		t.Fatalf("completed status = %#v, error = %v", status, err)
	}

	if err := store.MarkStarted(t.Context(), model.ApplicationRetentionTriggerManual, time.Now().UTC()); err != nil {
		t.Fatalf("mark started again: %v", err)
	}
	if err := store.RecoverInterrupted(t.Context()); err != nil {
		t.Fatalf("recover interrupted: %v", err)
	}
	_, status, err = store.Get(t.Context())
	if err != nil || status.Running || status.LastStatus != model.ApplicationRetentionStatusFailed ||
		status.LastErrorCode != interruptedApplicationRetentionCode {
		t.Fatalf("recovered status = %#v, error = %v", status, err)
	}
}

func TestApplicationRetentionDeletesHistoryInBoundedBatches(t *testing.T) {
	database, store := applicationRetentionFixture(t)
	now := model.NowUTC()
	admins := NewAdminStore(database.DB)
	user := testAdminUser("retention-owner", model.AdminRoleOwner)
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessions := []model.AdminSession{
		{ID: "active-session", UserID: user.ID, TokenHash: []byte("active"), CreatedAt: now, LastActivityAt: now,
			IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)},
		{ID: "expired-session", UserID: user.ID, TokenHash: []byte("expired"), CreatedAt: now.Add(-48 * time.Hour),
			LastActivityAt: now.Add(-48 * time.Hour), IdleExpiresAt: now.Add(-47 * time.Hour),
			AbsoluteExpiresAt: now.Add(-24 * time.Hour)},
		{ID: "revoked-session", UserID: user.ID, TokenHash: []byte("revoked"), CreatedAt: now.Add(-60 * 24 * time.Hour),
			LastActivityAt: now.Add(-40 * 24 * time.Hour), IdleExpiresAt: now.Add(-39 * 24 * time.Hour),
			AbsoluteExpiresAt: now.Add(-38 * 24 * time.Hour), RevokedAt: ptrTime(now.Add(-40 * 24 * time.Hour))},
		{ID: "recently-revoked", UserID: user.ID, TokenHash: []byte("recent"), CreatedAt: now.Add(-time.Hour),
			LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour),
			RevokedAt: ptrTime(now.Add(-time.Hour))},
	}
	for _, session := range sessions {
		copy := session
		if err := admins.CreateSession(t.Context(), &copy); err != nil {
			t.Fatalf("create session %s: %v", session.ID, err)
		}
	}
	seedAlertEvent(t, database, "resolved-old", model.AlertStatusResolved, now.Add(-200*24*time.Hour))
	seedAlertEvent(t, database, "resolved-recent", model.AlertStatusResolved, now.Add(-time.Hour))
	seedAlertEvent(t, database, "triggered-old", model.AlertStatusTriggered, now.Add(-200*24*time.Hour))
	seedAuditEvent(t, database, "audit-old", now.Add(-400*24*time.Hour))
	seedAuditEvent(t, database, "audit-recent", now.Add(-time.Hour))

	deleted, err := store.DeleteRevokedSessionsBefore(t.Context(), now, now.AddDate(0, 0, -30), model.ApplicationRetentionBatchSize)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted sessions = %d, error = %v", deleted, err)
	}
	deleted, err = store.DeleteResolvedAlertEventsBefore(t.Context(), now.AddDate(0, 0, -180), 1)
	if err != nil || deleted != 1 {
		t.Fatalf("deleted alert events = %d, error = %v", deleted, err)
	}
	deleted, err = store.DeleteResolvedAlertEventsBefore(t.Context(), now.AddDate(0, 0, -180), model.ApplicationRetentionBatchSize)
	if err != nil || deleted != 0 {
		t.Fatalf("second alert batch = %d, error = %v", deleted, err)
	}
	deleted, err = store.DeleteAuditEventsBefore(t.Context(), now.AddDate(0, 0, -180), model.ApplicationRetentionBatchSize)
	if err != nil || deleted != 1 {
		t.Fatalf("deleted audit events = %d, error = %v", deleted, err)
	}
	expectRemaining := []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM admin_sessions", 2},
		{"SELECT COUNT(*) FROM alert_events", 2},
		{"SELECT COUNT(*) FROM admin_audit_events WHERE id = 'audit-old'", 0},
		{"SELECT COUNT(*) FROM admin_audit_events WHERE id = 'audit-recent'", 1},
	}
	for _, expectation := range expectRemaining {
		var remaining int
		if err := database.DB.QueryRow(expectation.query).Scan(&remaining); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if remaining != expectation.want {
			t.Fatalf("%s = %d, want %d", expectation.query, remaining, expectation.want)
		}
	}
}

func TestApplicationRetentionDeleteFailuresAreReported(t *testing.T) {
	database, store := applicationRetentionFixture(t)
	if _, err := database.DB.Exec("DROP TABLE alert_events"); err != nil {
		t.Fatalf("drop alert events: %v", err)
	}
	if _, err := store.DeleteResolvedAlertEventsBefore(t.Context(), model.NowUTC(), 10); err == nil {
		t.Fatal("missing table must surface an error")
	}
	if _, err := store.DeleteAuditEventsBefore(t.Context(), model.NowUTC(), 10); err != nil {
		t.Fatalf("audit delete: %v", err)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func seedAlertEvent(t *testing.T, database *SQLiteStore, id string, status model.AlertStatus, triggeredAt time.Time) {
	t.Helper()
	_, err := database.DB.Exec(`INSERT INTO alert_events
		(id, rule_id, node_id, message, value, status, triggered_at, resolved_at, device_id, device_name)
		VALUES (?, ?, ?, '', 0, ?, ?, ?, '', '')`,
		id, "rule", "node", string(status), triggeredAt, triggeredAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("seed alert event: %v", err)
	}
}

func seedAuditEvent(t *testing.T, database *SQLiteStore, id string, createdAt time.Time) {
	t.Helper()
	_, err := database.DB.Exec(`INSERT INTO admin_audit_events
		(id, actor_id, actor_username, action, resource_type, resource_id, outcome, detail_json,
			request_id, ip_address, user_agent, session_prefix, created_at)
		VALUES (?, '', 'system', 'test', 'test', '', 'success', '{}', '', '', '', '', ?)`, id, createdAt)
	if err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
}

func TestApplicationRetentionModelValidation(t *testing.T) {
	settings := model.DefaultApplicationRetentionSettings()
	if err := settings.Validate(); err != nil {
		t.Fatalf("default settings invalid: %v", err)
	}
	settings.CleanupHourUTC = 24
	if err := settings.Validate(); err == nil {
		t.Fatal("invalid hour must be rejected")
	}
}
