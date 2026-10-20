//go:build linux

package adminauth

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestSecurityEventSourceRequiresOwnerRecentAndFullSourcePolicy(t *testing.T) {
	service, database, _ := securityFixture(t, false)
	owner := bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0").Principal
	var eventID string
	if err := database.DB.QueryRow(`SELECT id FROM security_events WHERE kind = ? LIMIT 1`,
		model.SecurityEventLoginSucceeded).Scan(&eventID); err != nil {
		t.Fatalf("read security event: %v", err)
	}

	until := service.now().Add(time.Hour)
	owner.Session.ReauthenticatedUntil = &until
	stale := owner
	stale.Session.ReauthenticatedUntil = nil
	if _, err := service.SecurityEventSource(t.Context(), &stale, eventID); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("stale reveal = %v", err)
	}
	if _, err := service.SecurityEventSource(t.Context(), &owner, eventID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("coarse policy reveal = %v", err)
	}
	settings, err := service.config.Store.SecurityNotificationSettings(t.Context())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	write := model.SecurityNotificationSettingsWrite{ExpectedRevision: settings.Revision,
		EnabledEventKinds: settings.EnabledEventKinds, PrivacyLevel: model.SecurityPrivacyFullSource,
		LookbackDays: settings.LookbackDays, ThresholdFailures: settings.ThresholdFailures,
		ThresholdWindowSecond: settings.ThresholdWindowSecond, InvalidTOTPThreshold: settings.InvalidTOTPThreshold,
		RetentionDays: settings.RetentionDays}
	if _, err := service.UpdateSecurityPolicy(t.Context(), &stale, write); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("stale policy update = %v", err)
	}
	if _, err := service.UpdateSecurityPolicy(t.Context(), &owner, write); err != nil {
		t.Fatalf("enable full source: %v", err)
	}

	administrator := owner
	administrator.User.Role = model.AdminRoleAdmin
	if _, err := service.SecurityEventSource(t.Context(), &administrator, eventID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("administrator reveal = %v", err)
	}
	if _, err := service.SecurityEventSource(t.Context(), &owner, ""); !errors.Is(err, store.ErrSecurityEventNotFound) {
		t.Fatalf("empty reveal = %v", err)
	}

	source, err := service.SecurityEventSource(t.Context(), &owner, eventID)
	if err != nil || source.Address != "203.0.113.47" || source.EventID != eventID {
		t.Fatalf("revealed source = %+v / %v", source, err)
	}
	var audited int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE action = 'security.event.source_reveal'`).
		Scan(&audited); err != nil {
		t.Fatalf("count reveal audit: %v", err)
	}
	if audited != 1 {
		t.Fatalf("reveal audit rows = %d, want 1", audited)
	}
	seedUnlinkedSecurityEvent(t, service.config.Store, database)
	if _, err := service.SecurityEventSource(t.Context(), &owner, "event-without-audit"); !errors.Is(err, store.ErrSecurityEventSourceUnavailable) {
		t.Fatalf("unlinked reveal = %v", err)
	}
}

// seedUnlinkedSecurityEvent 插入一条没有审计链接的事件，用于验证揭示失败路径。
func seedUnlinkedSecurityEvent(t *testing.T, admin *store.AdminStore, database *store.SQLiteStore) {
	t.Helper()
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin unlinked event: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	event := model.SecurityEvent{ID: "event-without-audit", Kind: model.SecurityEventLoginSuspicious,
		Severity: model.SecuritySeverityHigh, AuthMethod: model.SecurityAuthMethodUnknown,
		SourceClass: model.SecuritySourceClassPublic, SourceFamily: model.SecuritySourceFamilyIPv4,
		SourcePrefix: "203.0.113.0/24", BrowserFamily: "curl", OSFamily: "linux",
		DeviceClass: model.SecurityDeviceClassAutomation, Reasons: []string{model.SecurityReasonRateLimitLockout},
		PolicyRevision: 1, SourceKey: "key:event-without-audit", SourceKind: store.SecurityEventSourceKind,
		RoutingState: model.SecurityRoutingNotRouted, OccurredAt: now, RetainUntil: now.AddDate(0, 0, 180)}
	if err := admin.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed unlinked event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit unlinked event: %v", err)
	}
}
