package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type securityEventsFixture struct {
	handler  *AdminSecurityHandler
	service  *adminauth.Service
	admins   *store.AdminStore
	database *store.SQLiteStore
}

func newSecurityEventsFixture(t *testing.T) securityEventsFixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "security-events.db"))
	if err != nil {
		t.Fatalf("new security events store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	random := bytes.NewReader(bytes.Repeat([]byte{7}, 16384))
	service, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(database.DB),
		Secrets: testutil.NewSecrets(t, database.DB), LegacyToken: "bootstrap", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{MemoryKiB: 64, Iterations: 1,
			Parallelism: 1, SaltLength: 16, KeyLength: 32}, random)})
	if err != nil {
		t.Fatalf("new security service: %v", err)
	}
	return securityEventsFixture{handler: NewAdminSecurityHandler(service), service: service,
		admins: store.NewAdminStore(database.DB), database: database}
}

// source 直接调用来源揭示处理器，路由中间件不在本层用例范围内。
func (fixture securityEventsFixture) source(
	t *testing.T, principal *model.AdminPrincipal, id string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := newAdminRequest(t, principal, http.MethodGet, "/api/v1/admin/security/events/"+id+"/source", "")
	request.SetPathValue("id", id)
	response := httptest.NewRecorder()
	fixture.handler.HandleEventSource(response, request)
	return response
}

func (fixture securityEventsFixture) enableFullSource(t *testing.T, principal *model.AdminPrincipal) {
	t.Helper()
	settings, err := fixture.admins.SecurityNotificationSettings(t.Context())
	if err != nil {
		t.Fatalf("read security notification settings: %v", err)
	}
	write := model.SecurityNotificationSettingsWrite{ExpectedRevision: settings.Revision,
		EnabledEventKinds: settings.EnabledEventKinds, PrivacyLevel: model.SecurityPrivacyFullSource,
		LookbackDays: settings.LookbackDays, ThresholdFailures: settings.ThresholdFailures,
		ThresholdWindowSecond: settings.ThresholdWindowSecond, InvalidTOTPThreshold: settings.InvalidTOTPThreshold,
		RetentionDays: settings.RetentionDays}
	if _, err := fixture.service.UpdateSecurityPolicy(t.Context(), principal, write); err != nil {
		t.Fatalf("enable full source policy: %v", err)
	}
}

// seedSecurityEvent 写入一条安全事件；auditID 为空时模拟没有受保护来源记录的旧事件。
func seedSecurityEvent(t *testing.T, fixture securityEventsFixture, id string, auditID string) {
	t.Helper()
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	if auditID != "" {
		audit := model.AdminAuditEvent{ID: auditID, RequestID: "request-" + auditID, ActorUsername: "owner",
			Action:       "auth.login",
			ResourceType: "admin_session", ResourceID: "session-1", Outcome: model.AuditOutcomeSuccess,
			DetailJSON: "{}", IPAddress: "203.0.113.47", UserAgent: "curl/8.4.0",
			SessionPrefix: "private-prefix", CreatedAt: now}
		if err := fixture.admins.CreateAuditEvent(t.Context(), &audit); err != nil {
			t.Fatalf("seed audit event: %v", err)
		}
	}
	tx, err := fixture.database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin security event: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	event := model.SecurityEvent{ID: id, Kind: model.SecurityEventLoginSucceeded, Severity: model.SecuritySeverityMedium,
		AuthMethod: model.SecurityAuthMethodPassword, SourceClass: model.SecuritySourceClassPublic,
		SourceFamily: model.SecuritySourceFamilyIPv4, SourcePrefix: "203.0.113.0/24", BrowserFamily: "curl",
		OSFamily: "linux", DeviceClass: model.SecurityDeviceClassAutomation,
		Reasons: []string{model.SecurityReasonNewSourcePrefix}, PolicyRevision: 1, SourceKey: "key:" + id,
		SourceKind: store.SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		AuditEventID: auditID, OccurredAt: now, RetainUntil: now.AddDate(0, 0, 180)}
	if err := fixture.admins.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed security event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit security event: %v", err)
	}
}

func TestAdminSecurityEventSourceHandlerRejectsUnauthorizedRequests(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)

	unauthenticated := fixture.source(t, nil, "event-1")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated reveal = %d, %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	stale := owner
	stale.Session.ReauthenticatedUntil = nil
	if response := fixture.source(t, &stale, "event-1"); response.Code != http.StatusPreconditionRequired {
		t.Fatalf("stale reveal = %d, %s", response.Code, response.Body.String())
	}
	administrator := owner
	administrator.User.Role = model.AdminRoleAdmin
	if response := fixture.source(t, &administrator, "event-1"); response.Code != http.StatusForbidden {
		t.Fatalf("administrator reveal = %d, %s", response.Code, response.Body.String())
	}
	coarse := fixture.source(t, &owner, "event-1")
	if coarse.Code != http.StatusForbidden || !strings.Contains(coarse.Body.String(), "owner permission is required") {
		t.Fatalf("coarse reveal = %d, %s", coarse.Code, coarse.Body.String())
	}
}

func TestAdminSecurityEventSourceHandlerRevealsAuditedAddress(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	fixture.enableFullSource(t, &owner)
	seedSecurityEvent(t, fixture, "event-linked", "audit-linked")

	response := fixture.source(t, &owner, "event-linked")
	if response.Code != http.StatusOK {
		t.Fatalf("reveal = %d, %s", response.Code, response.Body.String())
	}
	var body struct {
		Data model.SecurityEventSource `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode reveal: %v", err)
	}
	if body.Data.EventID != "event-linked" || body.Data.Address != "203.0.113.47" || body.Data.CapturedAt.IsZero() {
		t.Fatalf("revealed source = %+v", body.Data)
	}
	page, err := fixture.admins.ListAuditEvents(t.Context(), model.AuditFilter{Action: "security.event.source_reveal"})
	if err != nil {
		t.Fatalf("list reveal audit events: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].ResourceID != "event-linked" || page.Events[0].ActorID != owner.User.ID {
		t.Fatalf("reveal audit = %+v", page)
	}
}

func TestAdminSecurityEventSourceHandlerReportsMissingProtectedRecord(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	fixture.enableFullSource(t, &owner)
	seedSecurityEvent(t, fixture, "event-unlinked", "")

	response := fixture.source(t, &owner, "event-unlinked")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "no protected source record for this event") {
		t.Fatalf("unlinked reveal = %d, %s", response.Code, response.Body.String())
	}
	missing := fixture.source(t, &owner, "event-absent")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing reveal = %d, %s", missing.Code, missing.Body.String())
	}
}
