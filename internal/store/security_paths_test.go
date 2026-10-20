package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func testSecurityKey(seed byte) []byte {
	key := make([]byte, 32)
	for index := range key {
		key[index] = seed
	}
	return key
}

func testThresholdEvent(id string, at time.Time) model.SecurityEvent {
	return model.SecurityEvent{ID: id, Kind: model.SecurityEventLoginSuspicious, Severity: model.SecuritySeverityHigh,
		AuthMethod: model.SecurityAuthMethodPassword, SourceClass: model.SecuritySourceClassPublic,
		SourceFamily: model.SecuritySourceFamilyIPv4, SourcePrefix: "203.0.113.0/24",
		BrowserFamily: "chrome", OSFamily: "linux", DeviceClass: model.SecurityDeviceClassDesktop,
		Reasons: []string{model.SecurityReasonRateLimitLockout}, PolicyRevision: 1, SourceKey: "key:" + id,
		SourceKind: SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		OccurredAt: at, RetainUntil: at.AddDate(0, 0, 180)}
}

func TestAdmitLoginFailureRejectsIncompleteAdmissions(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)

	replayed := securityTestAudit(now, "audit-without-request")
	replayed.RequestID = ""
	if _, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{Audit: replayed}); !errors.Is(err, ErrSecurityThresholdInvalid) {
		t.Fatalf("audit without request id = %v", err)
	}
	bumps := []ThresholdBump{
		{KeyKind: "unknown", KeyHash: testSecurityKey(1), Now: now, Window: time.Minute, Limit: 1, Lockout: time.Minute},
		{KeyKind: SecurityFingerprintKindSource, KeyHash: []byte("short"), Now: now, Window: time.Minute, Limit: 1, Lockout: time.Minute},
		{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(2), Window: time.Minute, Limit: 1, Lockout: time.Minute},
		{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(3), Now: now, Limit: 1, Lockout: time.Minute},
		{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(4), Now: now, Window: time.Minute, Lockout: time.Minute},
		{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(5), Now: now, Window: time.Minute, Limit: 1},
	}
	for index, bump := range bumps {
		admission := LoginFailureAdmission{Audit: securityTestAudit(now, fmt.Sprintf("audit-bump-%d", index)),
			Thresholds: []ThresholdBump{bump}}
		if _, err := store.AdmitLoginFailure(t.Context(), admission); !errors.Is(err, ErrSecurityThresholdInvalid) {
			t.Fatalf("bump %d error = %v", index, err)
		}
	}
	var audits int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE id LIKE 'audit-bump-%'`).Scan(&audits); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if audits != 0 {
		t.Fatalf("rejected admissions kept %d audit rows", audits)
	}
}

func TestThresholdLockoutKeepsExistingLockAndEmitsOnce(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	bump := ThresholdBump{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(9), Now: now,
		Window: 15 * time.Minute, Limit: 1, Lockout: 15 * time.Minute}
	outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "audit-limit-one"), Thresholds: []ThresholdBump{bump}})
	if err != nil || !outcomes[0].Locked || !outcomes[0].ShouldEmit {
		t.Fatalf("first failure = %+v / %v", outcomes, err)
	}
	key := ThresholdKey{KeyKind: outcomes[0].KeyKind, KeyHash: outcomes[0].KeyHash, WindowAt: outcomes[0].WindowAt}
	event := testThresholdEvent("event-threshold", now.Add(time.Minute))
	if marked, err := store.EmitThresholdEvent(t.Context(), key, event, now.Add(time.Minute)); err != nil || !marked {
		t.Fatalf("emit threshold event = %v / %v", marked, err)
	}
	if marked, err := store.EmitThresholdEvent(t.Context(), key, event, now.Add(time.Minute)); err != nil || marked {
		t.Fatalf("repeated emit = %v / %v", marked, err)
	}
	bump.Now = now.Add(2 * time.Minute)
	outcomes, err = store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(bump.Now, "audit-second-failure"), Thresholds: []ThresholdBump{bump}})
	if err != nil || !outcomes[0].Locked || outcomes[0].ShouldEmit || outcomes[0].Count != 2 {
		t.Fatalf("windowed lockout = %+v / %v", outcomes, err)
	}
}

func TestEmitThresholdEventRollsBackUnmarkedWindow(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	missing := ThresholdKey{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(7), WindowAt: now}
	if marked, err := store.EmitThresholdEvent(t.Context(), missing, testThresholdEvent("event-missing", now), now); err != nil || marked {
		t.Fatalf("unmarked window = %v / %v", marked, err)
	}

	bump := ThresholdBump{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(8), Now: now,
		Window: time.Hour, Limit: 5, Lockout: time.Minute}
	outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "audit-open-window"), Thresholds: []ThresholdBump{bump}})
	if err != nil || outcomes[0].Locked {
		t.Fatalf("open window = %+v / %v", outcomes, err)
	}
	key := ThresholdKey{KeyKind: outcomes[0].KeyKind, KeyHash: outcomes[0].KeyHash, WindowAt: outcomes[0].WindowAt}
	invalid := testThresholdEvent("event-invalid", now)
	invalid.Kind = "security.unknown"
	if marked, err := store.EmitThresholdEvent(t.Context(), key, invalid, now); err == nil || marked {
		t.Fatalf("invalid threshold event = %v / %v", marked, err)
	}
	if marked, err := store.EmitThresholdEvent(t.Context(), key, testThresholdEvent("event-valid", now), now); err != nil || !marked {
		t.Fatalf("valid threshold event after rollback = %v / %v", marked, err)
	}
}

func TestKnownSecurityFingerprintUsesLookbackWindow(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	fingerprint := testSecurityKey(11)
	owner := testAdminUser("owner", model.AdminRoleOwner)
	if err := store.CreateUser(t.Context(), &owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin fingerprint: %v", err)
	}
	if err := upsertSecurityFingerprints(t.Context(), tx, []SecurityFingerprint{{ActorUserID: "owner",
		Epoch: "epoch-1", Kind: SecurityFingerprintKindSource, Fingerprint: fingerprint,
		SeenAt: now.UnixMilli(), RetainUntil: now.AddDate(0, 0, 180).UnixMilli()}}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed fingerprint: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit fingerprint: %v", err)
	}

	known, err := store.KnownSecurityFingerprint(t.Context(), "owner", "epoch-1",
		SecurityFingerprintKindSource, fingerprint, now.Add(-time.Hour).UnixMilli())
	if err != nil || !known {
		t.Fatalf("known fingerprint = %v / %v", known, err)
	}
	stale, err := store.KnownSecurityFingerprint(t.Context(), "owner", "epoch-1",
		SecurityFingerprintKindSource, fingerprint, now.Add(time.Hour).UnixMilli())
	if err != nil || stale {
		t.Fatalf("fingerprint outside window = %v / %v", stale, err)
	}
	if total, err := store.CountSecurityFingerprints(t.Context()); err != nil || total != 1 {
		t.Fatalf("fingerprint count = %d / %v", total, err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := store.CountSecurityFingerprints(t.Context()); err == nil {
		t.Fatal("closed store counted fingerprints")
	}
}

func TestMarkSecurityEventRoutingPersistsValidState(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	seedSecurityEvent(t, store, "event-routing", model.SecurityEventLoginSucceeded, model.SecuritySeverityInfo,
		model.SecurityReasonNewSourcePrefix, model.SecuritySourceClassPublic, "203.0.113.0/24", now)

	if err := store.MarkSecurityEventRouting(t.Context(), "event-routing", "unknown-state"); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("invalid routing = %v", err)
	}
	if err := store.MarkSecurityEventRouting(t.Context(), "event-routing", model.SecurityRoutingRouted); err != nil {
		t.Fatalf("mark routing: %v", err)
	}
	event, err := store.GetSecurityEvent(t.Context(), "event-routing")
	if err != nil || event.RoutingState != model.SecurityRoutingRouted {
		t.Fatalf("routing state = %+v / %v", event, err)
	}
}

func TestListSecurityEventsValidatesFiltersAndPagination(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	seedSecurityEvent(t, store, "event-one", model.SecurityEventLoginSucceeded, model.SecuritySeverityInfo,
		model.SecurityReasonNewSourcePrefix, model.SecuritySourceClassPublic, "203.0.113.0/24", now)
	seedSecurityEvent(t, store, "event-two", model.SecurityEventLoginSuspicious, model.SecuritySeverityHigh,
		model.SecurityReasonRateLimitLockout, model.SecuritySourceClassPublic, "198.51.100.0/24", now.Add(time.Minute))

	page, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Kind: model.SecurityEventLoginSuspicious})
	if err != nil || page.Total != 1 || len(page.Events) != 1 || page.Events[0].ID != "event-two" {
		t.Fatalf("kind filter page = %+v / %v", page, err)
	}
	page, err = store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || len(page.Events) != 1 || page.Events[0].ID != "event-one" {
		t.Fatalf("pagination = %+v / %v", page, err)
	}
	page, err = store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{DeliveryState: "none"})
	if err != nil || page.Total != 2 {
		t.Fatalf("delivery filter = %+v / %v", page, err)
	}
	for name, filter := range map[string]model.SecurityEventFilter{
		"severity": {Severity: "critical"},
		"delivery": {DeliveryState: "unknown"},
		"reason":   {Reason: "unknown-reason"},
	} {
		if _, err := store.ListSecurityEvents(t.Context(), filter); !errors.Is(err, ErrSecurityFilterInvalid) {
			t.Fatalf("%s filter error = %v", name, err)
		}
	}
}
