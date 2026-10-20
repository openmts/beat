package adminauth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestRecordServiceAuditStoresServiceActorAndBoundsFields(t *testing.T) {
	service := newTestService(t)
	oversized := strings.Repeat("x", 400)
	err := service.RecordServiceAudit(t.Context(), model.ServiceAuditEvent{
		RequestID: "request-service", PrincipalID: "principal-one", PrincipalName: oversized,
		Action: "service_token.request", ResourceType: "service_scope", ResourceID: "audit:read",
		Outcome: model.AuditOutcomeFailure, Reason: "scope", Scope: "audit:read",
		Method: http.MethodGet, Path: "/api/v1/admin/audit-events?token=" + oversized,
		IPAddress: "192.0.2.10", UserAgent: oversized,
	})
	if err != nil {
		t.Fatalf("record service audit: %v", err)
	}
	page, err := service.config.Store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("audit events = %d", len(page.Events))
	}
	event := page.Events[0]
	if event.ActorType != model.AuditActorService || event.ActorID != "principal-one" {
		t.Fatalf("event = %+v", event)
	}
	if event.SessionPrefix != "" || event.RequestID != "request-service" {
		t.Fatalf("event = %+v", event)
	}
	if event.Outcome != model.AuditOutcomeFailure || !strings.Contains(event.DetailJSON, `"reason":"scope"`) {
		t.Fatalf("detail = %q", event.DetailJSON)
	}
	for name, value := range map[string]string{
		"actor": event.ActorUsername, "agent": event.UserAgent, "detail": event.DetailJSON,
	} {
		if strings.Contains(value, strings.Repeat("x", 300)) {
			t.Fatalf("%s field was not bounded: %q", name, value)
		}
	}
}
