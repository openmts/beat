package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestSecurityEventHistoryFiltersAndAcknowledgement(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	seedSecurityEvent(t, store, "event-public", model.SecurityEventLoginSuspicious, model.SecuritySeverityHigh,
		model.SecurityReasonRateLimitLockout, model.SecuritySourceClassPublic, "203.0.113.0/24", now)
	seedSecurityEvent(t, store, "event-private", model.SecurityEventLoginSucceeded, model.SecuritySeverityInfo,
		"", model.SecuritySourceClassPrivate, "private", now.Add(time.Second))

	page, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Kind: model.SecurityEventLoginSuspicious})
	if err != nil || page.Total != 1 || len(page.Events) != 1 || page.Events[0].ID != "event-public" {
		t.Fatalf("filtered history = %v / %v", page, err)
	}
	reasons, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Reason: model.SecurityReasonRateLimitLockout})
	if err != nil || reasons.Total != 1 {
		t.Fatalf("reason filter = %v / %v", reasons, err)
	}
	if _, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Severity: "critical"}); !errors.Is(err, ErrSecurityFilterInvalid) {
		t.Fatalf("unknown severity filter = %v", err)
	}
	detail, err := store.GetSecurityEvent(t.Context(), "event-public")
	if err != nil || detail.DeliveryState != "none" || detail.SourcePrefix != "203.0.113.0/24" {
		t.Fatalf("event detail = %v / %v", detail, err)
	}
	if _, err := store.GetSecurityEvent(t.Context(), "missing"); !errors.Is(err, ErrSecurityEventNotFound) {
		t.Fatalf("missing event = %v", err)
	}

	change := SecurityEventAck{EventID: "event-public", ActorID: "owner", Note: "reviewed", At: now.Add(time.Minute),
		Audit: securityTestAudit(now, "audit-ack")}
	acknowledged, err := store.AcknowledgeSecurityEvent(t.Context(), change)
	if err != nil || acknowledged.AcknowledgedBy != "owner" || acknowledged.AcknowledgedNote != "reviewed" {
		t.Fatalf("acknowledged event = %v / %v", acknowledged, err)
	}
	if _, err := store.AcknowledgeSecurityEvent(t.Context(), change); !errors.Is(err, ErrSecurityEventAcknowledged) {
		t.Fatalf("repeat acknowledgement = %v", err)
	}
}

func TestSecuritySettingsUpdateRequiresRevisionAndAudits(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	settings, err := store.SecurityNotificationSettings(t.Context())
	if err != nil || settings.Revision != 1 || settings.PrivacyLevel != "coarse" {
		t.Fatalf("default settings = %v / %v", settings, err)
	}
	if credentials, err := store.SecurityCredentials(t.Context()); err != nil || len(credentials.FingerprintKey) != 32 {
		t.Fatalf("credentials = %v / %v", credentials, err)
	}
	write := model.SecurityNotificationSettingsWrite{ExpectedRevision: settings.Revision,
		EnabledEventKinds: []string{model.SecurityEventLoginSuspicious}, PrivacyLevel: "full_source",
		LookbackDays: 14, ThresholdFailures: 3, ThresholdWindowSecond: 600, InvalidTOTPThreshold: 2, RetentionDays: 60}
	updated, err := store.UpdateSecurityNotificationSettings(t.Context(), SecuritySettingsChange{Write: write,
		Audit: securityTestAudit(now, "audit-policy"), UpdatedBy: "owner", Now: now})
	if err != nil || updated.Revision != 2 || updated.PrivacyLevel != "full_source" || updated.LookbackDays != 14 {
		t.Fatalf("updated settings = %v / %v", updated, err)
	}
	if _, err := store.UpdateSecurityNotificationSettings(t.Context(), SecuritySettingsChange{Write: write,
		Audit: securityTestAudit(now, "audit-policy-2"), UpdatedBy: "owner", Now: now}); !errors.Is(err, ErrSecuritySettingsRevision) {
		t.Fatalf("stale revision = %v", err)
	}
	invalid := write
	invalid.ExpectedRevision = updated.Revision
	invalid.EnabledEventKinds = []string{"security.unknown"}
	if _, err := store.UpdateSecurityNotificationSettings(t.Context(), SecuritySettingsChange{Write: invalid,
		Audit: securityTestAudit(now, "audit-policy-3"), UpdatedBy: "owner", Now: now}); !errors.Is(err, ErrSecuritySettingsInvalid) {
		t.Fatalf("invalid kinds = %v", err)
	}
	var audits int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE id='audit-policy'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("policy audit = %d / %v", audits, err)
	}
}

func TestSecurityThresholdLocksAndPurges(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	bump := ThresholdBump{KeyKind: SecurityFingerprintKindSource, KeyHash: make([]byte, 32),
		Now: now, Window: 15 * time.Minute, Limit: 5, Lockout: 15 * time.Minute}
	for attempt := 0; attempt < 4; attempt++ {
		outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit: securityTestAudit(now, fmt.Sprintf("audit-failure-%d", attempt)), Thresholds: []ThresholdBump{bump}})
		if err != nil || outcomes[0].Locked || outcomes[0].ShouldEmit {
			t.Fatalf("threshold attempt %d = %v / %v", attempt, outcomes, err)
		}
	}
	outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "audit-lock"), Thresholds: []ThresholdBump{bump}})
	if err != nil || !outcomes[0].Locked || !outcomes[0].ShouldEmit {
		t.Fatalf("lockout outcome = %v / %v", outcomes, err)
	}
	key := ThresholdKey{KeyKind: outcomes[0].KeyKind, KeyHash: outcomes[0].KeyHash, WindowAt: outcomes[0].WindowAt}
	locked, err := store.SecurityLockActive(t.Context(), []LoginThresholdKey{{KeyKind: bump.KeyKind, KeyHash: bump.KeyHash}}, now)
	if err != nil || !locked {
		t.Fatalf("lock state = %v / %v", locked, err)
	}
	if err := store.ClearSecurityThresholds(t.Context(), []LoginThresholdKey{{KeyKind: bump.KeyKind, KeyHash: bump.KeyHash}}); err != nil {
		t.Fatalf("clear thresholds: %v", err)
	}
	if total, err := store.CountSecurityThresholds(t.Context()); err != nil || total != 0 {
		t.Fatalf("threshold rows = %d / %v", total, err)
	}
	if _, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{Audit: securityTestAudit(now, "audit-replay"),
		Thresholds: []ThresholdBump{bump}}); err != nil {
		t.Fatalf("threshold replay: %v", err)
	}
	if purged, err := store.PurgeSecurityData(t.Context(), now.AddDate(0, 0, 400), 100); err != nil || purged == 0 {
		t.Fatalf("purge = %d / %v", purged, err)
	}
	if err := store.MarkSecurityEventRouting(t.Context(), "missing", "routed"); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("mark routing = %v", err)
	}
	if _, _, err := store.SecurityOutboxAge(t.Context(), now); err != nil {
		t.Fatalf("outbox age: %v", err)
	}
	_ = key
}

func TestSecurityEventInsertValidatesVocabulary(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	invalid := model.SecurityEvent{ID: "event", Kind: "security.unknown", Severity: model.SecuritySeverityInfo,
		AuthMethod: model.SecurityAuthMethodPassword, SourceClass: model.SecuritySourceClassUnknown,
		SourceFamily: model.SecuritySourceFamilyUnknown, DeviceClass: model.SecurityDeviceClassUnknown,
		SourceKey: "key", SourceKind: SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		OccurredAt: time.Now().UTC(), RetainUntil: time.Now().UTC().Add(time.Hour)}
	if err := store.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{invalid}); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("invalid event = %v", err)
	}
}

func seedSecurityEvent(
	t *testing.T, store *AdminStore, id, kind, severity, reason, class, prefix string, at time.Time,
) {
	t.Helper()
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	reasons := []string{}
	if reason != "" {
		reasons = append(reasons, reason)
	}
	event := model.SecurityEvent{ID: id, Kind: kind, Severity: severity, AuthMethod: model.SecurityAuthMethodPassword,
		SourceClass: class, SourceFamily: model.SecuritySourceFamilyIPv4, SourcePrefix: prefix,
		BrowserFamily: "chrome", OSFamily: "linux", DeviceClass: model.SecurityDeviceClassDesktop,
		Reasons: reasons, PolicyRevision: 1, SourceKey: "key:" + id, SourceKind: SecurityEventSourceKind,
		RoutingState: model.SecurityRoutingNotRouted, OccurredAt: at, RetainUntil: at.AddDate(0, 0, 180)}
	if err := store.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed security event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func securityTestAudit(at time.Time, id string) model.AdminAuditEvent {
	return model.AdminAuditEvent{ID: id, RequestID: "request-" + id, ActorType: model.AuditActorAdmin,
		Action: "security.test", ResourceType: "security_event", Outcome: model.AuditOutcomeSuccess,
		DetailJSON: "{}", CreatedAt: at}
}
