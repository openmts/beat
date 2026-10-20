//go:build linux

package adminauth

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestSecurityEventConsoleRequiresActiveAdministrator(t *testing.T) {
	service, _, _ := securityFixture(t, false)
	owner := bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0").Principal
	disabled := owner
	disabled.User.Enabled = false

	if _, err := service.ListSecurityEvents(t.Context(), nil, model.SecurityEventFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nil list = %v", err)
	}
	if _, err := service.ListSecurityEvents(t.Context(), &disabled, model.SecurityEventFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled list = %v", err)
	}
	if _, err := service.SecurityEvent(t.Context(), &disabled, "event-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled detail = %v", err)
	}
	if _, err := service.SecurityEvent(t.Context(), &owner, ""); !errors.Is(err, store.ErrSecurityEventNotFound) {
		t.Fatalf("empty detail = %v", err)
	}
	if _, err := service.AcknowledgeSecurityEvent(t.Context(), &disabled, "event-1", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled acknowledge = %v", err)
	}
}

func TestSecurityEventConsoleListsDetailsAndAcknowledgesOnce(t *testing.T) {
	service, database, _ := securityFixture(t, true)
	owner := bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0").Principal

	page, err := service.ListSecurityEvents(t.Context(), &owner, model.SecurityEventFilter{Limit: 10})
	if err != nil || page.Total == 0 || len(page.Events) == 0 {
		t.Fatalf("security event page = %+v / %v", page, err)
	}
	event := page.Events[0]
	detail, err := service.SecurityEvent(t.Context(), &owner, event.ID)
	if err != nil || detail.ID != event.ID || detail.Kind != event.Kind {
		t.Fatalf("security event detail = %+v / %v", detail, err)
	}
	if _, err := service.SecurityEvent(t.Context(), &owner, "missing-event"); !errors.Is(err, store.ErrSecurityEventNotFound) {
		t.Fatalf("missing detail = %v", err)
	}

	acknowledged, err := service.AcknowledgeSecurityEvent(t.Context(), &owner, event.ID, "investigated")
	if err != nil || acknowledged.AcknowledgedBy != owner.User.ID || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("acknowledged = %+v / %v", acknowledged, err)
	}
	if acknowledged.AcknowledgedNote != "investigated" {
		t.Fatalf("acknowledged note = %q", acknowledged.AcknowledgedNote)
	}
	if _, err := service.AcknowledgeSecurityEvent(t.Context(), &owner, event.ID, "again"); !errors.Is(err, store.ErrSecurityEventAcknowledged) {
		t.Fatalf("repeated acknowledge = %v", err)
	}
	var audited int
	if err := database.DB.QueryRow(
		`SELECT COUNT(*) FROM admin_audit_events WHERE action = 'security.event.acknowledge'`).Scan(&audited); err != nil {
		t.Fatalf("count acknowledge audit: %v", err)
	}
	if audited != 1 {
		t.Fatalf("acknowledge audit rows = %d, want 1", audited)
	}
}

func TestSecurityMetricsSnapshotCopiesCountersAndHandlesMissingCollector(t *testing.T) {
	service, _, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0")

	// 首次登录同时写入成功事件与“新来源/新设备”可疑事件。
	snapshot := service.SecurityMetricsSnapshot()
	if snapshot.Kinds[model.SecurityEventLoginSucceeded] != 1 || snapshot.Kinds[model.SecurityEventLoginSuspicious] != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Severities) != 2 || len(snapshot.Reasons) == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	snapshot.Kinds[model.SecurityEventLoginSucceeded] = 99
	if again := service.SecurityMetricsSnapshot(); again.Kinds[model.SecurityEventLoginSucceeded] != 1 {
		t.Fatalf("snapshot aliased internal counters: %+v", again)
	}
	service.observeSecurityTransactionFailure()
	service.observeSecurityOutboxFailure()
	counted := service.SecurityMetricsSnapshot()
	if counted.TransactionFailures != 1 || counted.OutboxFailures != 1 {
		t.Fatalf("failure counters = %+v", counted)
	}

	unstarted := &Service{}
	if empty := unstarted.SecurityMetricsSnapshot(); empty.Kinds != nil || empty.TransactionFailures != 0 {
		t.Fatalf("unstarted snapshot = %+v", empty)
	}
	unstarted.observeSecurityEvents(nil)
	unstarted.observeSecurityTransactionFailure()
	unstarted.observeSecurityOutboxFailure()
}

func TestSecurityDiagnosticsAndOutboxAgeUsePersistedState(t *testing.T) {
	service, _, _ := securityFixture(t, true)
	bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0")

	diagnostics, err := service.SecurityMetrics(t.Context())
	if err != nil || diagnostics.Fingerprints == 0 {
		t.Fatalf("security diagnostics = %+v / %v", diagnostics, err)
	}
	// 没有配置通知渠道时没有待投递行，年龄查询返回 ok=false 而不是错误。
	age, ok, err := service.SecurityOutboxAge(t.Context(), service.now())
	if err != nil || age != 0 {
		t.Fatalf("outbox age = %v / %v / %v", age, ok, err)
	}
	if ok {
		t.Fatalf("outbox age without routed notifications = %v, want no pending delivery", age)
	}
}

func TestListAuditEventsForServiceClearsSessionPrefixAndCarriesMetadata(t *testing.T) {
	service, _, _ := securityFixture(t, false)
	bootstrapTestOwner(t, service, "203.0.113.47", "curl/8.4.0")

	page, err := service.ListAuditEventsForService(t.Context(), model.AuditFilter{Limit: 10})
	if err != nil || len(page.Events) == 0 {
		t.Fatalf("service audit page = %+v / %v", page, err)
	}
	for _, event := range page.Events {
		if event.SessionPrefix != "" {
			t.Fatalf("service audit leaked a session prefix: %+v", event)
		}
	}

	metadata := AuditMetadata{RequestID: "request-metadata", IPAddress: "198.51.100.7", UserAgent: "beat-agent/1"}
	ctx := WithAuditMetadata(t.Context(), metadata)
	if err := service.RecordServiceAudit(ctx, model.ServiceAuditEvent{Action: "service_token.request",
		ResourceType: "service_scope", ResourceID: "audit:read", Outcome: model.AuditOutcomeSuccess}); err != nil {
		t.Fatalf("record service audit: %v", err)
	}
	servicePage, err := service.ListAuditEventsForService(t.Context(),
		model.AuditFilter{Action: "service_token.request", Limit: 5})
	if err != nil || len(servicePage.Events) != 1 {
		t.Fatalf("service audit page = %+v / %v", servicePage, err)
	}
	event := servicePage.Events[0]
	if event.RequestID != metadata.RequestID || event.IPAddress != metadata.IPAddress || event.UserAgent != metadata.UserAgent {
		t.Fatalf("service audit metadata = %+v", event)
	}
}
