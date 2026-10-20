//go:build linux

package adminauth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type securityTestSender struct{ dispatched int }

func (sender *securityTestSender) Send(context.Context, model.NotificationDispatch) model.NotificationSendResult {
	sender.dispatched++
	return model.NotificationSendResult{Success: true}
}

// securityFixture 构造带真实通知队列或空队列的认证服务。
func securityFixture(t *testing.T, withQueue bool) (*Service, *store.SQLiteStore, *securityTestSender) {
	t.Helper()
	database, secrets := authDependencies(t)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	config := ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets,
		LegacyToken: "bootstrap-secret",
		Passwords: NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1,
			SaltLength: 16, KeyLength: 32}, rand.Reader),
		Now: func() time.Time { return now }}
	sender := &securityTestSender{}
	if withQueue {
		queue, err := notificationqueue.NewService(notificationqueue.Config{
			Database: database.DB, Secrets: secrets, Sender: sender})
		if err != nil {
			t.Fatalf("new notification queue: %v", err)
		}
		config.Queue = queue
	}
	service, err := NewService(config)
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	return service, database, sender
}

func bootstrapTestOwner(t *testing.T, service *Service, ipAddress, userAgent string) LoginResult {
	t.Helper()
	result, err := service.Bootstrap(t.Context(), BootstrapRequest{BootstrapToken: "bootstrap-secret",
		Username: "owner", DisplayName: "Owner account", Password: pendingTestPassword,
		IPAddress: ipAddress, UserAgent: userAgent})
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	return result
}

func securityEventKinds(t *testing.T, database *store.SQLiteStore) map[string]string {
	t.Helper()
	rows, err := database.DB.Query(`SELECT kind, routing_state FROM security_events`)
	if err != nil {
		t.Fatalf("query security events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	kinds := map[string]string{}
	for rows.Next() {
		var kind, routing string
		if err := rows.Scan(&kind, &routing); err != nil {
			t.Fatalf("scan security event: %v", err)
		}
		kinds[kind] = routing
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate security events: %v", err)
	}
	return kinds
}

func TestLoginSecurityCommitsEventFingerprintsAndOutbox(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "203.0.113.44", "Mozilla/5.0 Chrome/120.0 Windows NT 10.0")

	kinds := securityEventKinds(t, database)
	if kinds[model.SecurityEventLoginSucceeded] != model.SecurityRoutingNotRouted {
		t.Fatalf("successful login event routing = %v", kinds)
	}
	if _, ok := kinds[model.SecurityEventLoginSuspicious]; !ok {
		t.Fatal("new source and device did not produce a suspicious event")
	}
	var severity, reasons, prefix string
	if err := database.DB.QueryRow(`SELECT severity, reasons_json, source_prefix FROM security_events
		WHERE kind='security.login_suspicious'`).Scan(&severity, &reasons, &prefix); err != nil {
		t.Fatalf("read suspicious event: %v", err)
	}
	if severity != model.SecuritySeverityHigh || !strings.Contains(reasons, model.SecurityReasonNewSourcePrefix) ||
		prefix != "203.0.113.0/24" {
		t.Fatalf("suspicious event = %s %s %s", severity, reasons, prefix)
	}
	var fingerprints int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM security_login_fingerprints`).Scan(&fingerprints); err != nil {
		t.Fatalf("count fingerprints: %v", err)
	}
	if fingerprints != 2 {
		t.Fatalf("fingerprint rows = %d, want 2", fingerprints)
	}
	var messages, deliveries int
	var sealed []byte
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM notification_messages WHERE source_kind='security_event'`).
		Scan(&messages); err != nil {
		t.Fatalf("count security messages: %v", err)
	}
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&deliveries); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if err := database.DB.QueryRow(`SELECT payload_encrypted FROM notification_messages
		WHERE source_kind='security_event' LIMIT 1`).Scan(&sealed); err != nil {
		t.Fatalf("read sealed payload: %v", err)
	}
	if messages != 2 || deliveries != 0 {
		t.Fatalf("messages = %d, deliveries = %d", messages, deliveries)
	}
	if strings.Contains(string(sealed), "Owner account") || strings.Contains(string(sealed), "203.0.113.44") {
		t.Fatal("sealed payload leaked plaintext identity")
	}
}

func TestLoginSecurityFailsClosedWhenEventWriteFails(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "", "")
	if _, err := database.DB.Exec(`CREATE TRIGGER reject_security_event BEFORE INSERT ON security_events
		BEGIN SELECT RAISE(ABORT, 'injected security failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword})
	if err == nil || result.Token != "" {
		t.Fatal("login succeeded without its security event")
	}
	if _, err := database.DB.Exec("DROP TRIGGER reject_security_event"); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_sessions").Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("failed login persisted sessions: %d", sessions)
	}
}

func TestLoginSecurityThresholdsSurviveRestart(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "198.51.100.7", "curl/8.4.0")
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := service.Login(t.Context(), LoginRequest{Username: "owner",
			Password: "wrong password value", IPAddress: "198.51.100.7", UserAgent: "curl/8.4.0"}); err == nil {
			t.Fatal("failed login accepted")
		}
	}
	var lockouts int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM security_events
		WHERE kind='security.login_suspicious' AND reasons_json LIKE ?`,
		"%"+model.SecurityReasonRateLimitLockout+"%").Scan(&lockouts); err != nil {
		t.Fatalf("count lockout events: %v", err)
	}
	if lockouts == 0 {
		t.Fatal("threshold crossing produced no suspicious event")
	}
	restarted, err := NewService(ServiceConfig{Store: service.config.Store, Secrets: service.config.Secrets,
		LegacyToken: "bootstrap-secret", Passwords: service.config.Passwords, Now: service.config.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = restarted.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword,
		IPAddress: "198.51.100.7", UserAgent: "curl/8.4.0"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("restarted lock state = %v", err)
	}
}

func TestLoginSecurityKeepsSourcePrefixPrivate(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "10.1.2.3", "Mozilla/5.0 (X11; Linux x86_64) Firefox/121.0")
	var prefix, userAgent string
	if err := database.DB.QueryRow(`SELECT source_prefix, browser_family FROM security_events
		LIMIT 1`).Scan(&prefix, &userAgent); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if prefix != "private" || userAgent != "firefox" {
		t.Fatalf("privacy fields = %s / %s", prefix, userAgent)
	}
	var stored string
	if err := database.DB.QueryRow(`SELECT COALESCE(MAX(browser_family || source_prefix || actor_display_name), '')
		FROM security_events`).Scan(&stored); err != nil {
		t.Fatalf("read event text: %v", err)
	}
	if strings.Contains(stored, "10.1.2.3") || strings.Contains(stored, "Firefox/121") {
		t.Fatal("security event stored raw request values")
	}
}

func TestLoginSecuritySkipsOutboxWhenKindDisabled(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	owner := bootstrapTestOwner(t, service, "", "").Principal
	settings, err := service.config.Store.SecurityNotificationSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	until := service.now().Add(time.Hour)
	owner.Session.ReauthenticatedUntil = &until
	if _, err := service.UpdateSecurityPolicy(t.Context(), &owner, model.SecurityNotificationSettingsWrite{
		ExpectedRevision: settings.Revision, EnabledEventKinds: []string{model.SecurityEventLoginSucceeded},
		PrivacyLevel: "coarse", LookbackDays: settings.LookbackDays, ThresholdFailures: settings.ThresholdFailures,
		ThresholdWindowSecond: settings.ThresholdWindowSecond, InvalidTOTPThreshold: settings.InvalidTOTPThreshold,
		RetentionDays: settings.RetentionDays}); err != nil {
		t.Fatalf("update security policy: %v", err)
	}
	result, err := service.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword,
		IPAddress: "203.0.113.9", UserAgent: "curl/8.4.0"})
	if err != nil || result.Token == "" {
		t.Fatalf("login with disabled suspicious kind = %v", err)
	}
	sourceKey := "login-suspicious:" + result.Principal.Session.ID
	var routing string
	if err := database.DB.QueryRow(`SELECT routing_state FROM security_events
		WHERE kind='security.login_suspicious' AND source_key = ?`, sourceKey).Scan(&routing); err != nil {
		t.Fatalf("read skipped event: %v", err)
	}
	if routing != model.SecurityRoutingSkipped {
		t.Fatalf("disabled kind routing = %s", routing)
	}
	var messages int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM notification_messages
		WHERE source_kind='security_event' AND source_key = ?`, sourceKey).Scan(&messages); err != nil {
		t.Fatalf("count suspicious messages: %v", err)
	}
	if messages != 0 {
		t.Fatalf("disabled kind queued %d messages", messages)
	}
}

func TestSecurityNotificationPolicyRequiresOwner(t *testing.T) {
	service, _, _ := securityFixture(t, false)
	admin := model.AdminPrincipal{User: model.AdminUser{ID: "admin", Role: model.AdminRoleAdmin, Enabled: true}}
	if _, err := service.SecurityPolicy(t.Context(), &admin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin policy read = %v", err)
	}
	if _, err := service.UpdateSecurityPolicy(t.Context(), &admin, model.SecurityNotificationSettingsWrite{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin policy write = %v", err)
	}
	owner := recentOwnerPrincipal()
	if _, err := service.SecurityPolicy(t.Context(), &owner); err != nil {
		t.Fatalf("owner policy read = %v", err)
	}
	if _, err := service.UpdateSecurityPolicy(t.Context(), &owner, model.SecurityNotificationSettingsWrite{}); !errors.Is(err, store.ErrSecuritySettingsInvalid) {
		t.Fatalf("owner policy write validation = %v", err)
	}
}
