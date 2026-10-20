package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
)

type recordedServiceTokenAudit struct{ events []model.ServiceAuditEvent }

func (audit *recordedServiceTokenAudit) RecordServiceAudit(_ context.Context, event model.ServiceAuditEvent) error {
	audit.events = append(audit.events, event)
	return nil
}

func runAuditedServiceTokenRequest(
	t *testing.T, config ServiceTokenAccessConfig, method, target string, next http.Handler,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := ServiceTokenAccess(config)(next)
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer beat_service_v1_abcdefghijklmnop")
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func serviceTokenOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestServiceTokenAuditRecordsPrivilegedSuccessWithoutSecret(t *testing.T) {
	audit, metrics := &recordedServiceTokenAudit{}, &recordedServiceTokenMetrics{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipalWithVisibility(
			model.ServiceVisibilityPrivate, []model.ServiceScope{model.ServiceScopeAuditRead}, nil)},
		Audit: audit, Metrics: metrics, Scope: model.ServiceScopeAuditRead, RequirePrivate: true,
	}
	response := runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/admin/audit-events?token=ignored", serviceTokenOKHandler())
	if response.Code != http.StatusOK || len(audit.events) != 1 {
		t.Fatalf("response = %d, events = %d", response.Code, len(audit.events))
	}
	event := audit.events[0]
	if event.Outcome != model.AuditOutcomeSuccess || event.PrincipalID != "principal-one" ||
		event.PrincipalName != "nightly" || event.Action != serviceTokenAuditAction {
		t.Fatalf("event = %+v", event)
	}
	if event.Scope != string(model.ServiceScopeAuditRead) || event.Path != "/api/v1/admin/audit-events" {
		t.Fatalf("event = %+v", event)
	}
	for _, value := range []string{event.PrincipalName, event.Path, event.UserAgent, event.ResourceID, event.Scope, event.Reason} {
		if strings.Contains(value, "abcdefghijklmnop") {
			t.Fatalf("audit leaked token material: %q", value)
		}
	}
	if len(metrics.latencies) != 1 || metrics.latencies[0] != string(model.ServiceScopeAuditRead) {
		t.Fatalf("latency scopes = %v", metrics.latencies)
	}
}

func TestServiceTokenAuditSkipsRoutineReadsAndRecordsDenials(t *testing.T) {
	audit := &recordedServiceTokenAudit{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipal(
			[]model.ServiceScope{model.ServiceScopeMetricsRead}, nil)},
		Audit: audit, Scope: model.ServiceScopeMetricsRead,
	}
	response := runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/nodes/node-one/metrics", serviceTokenOKHandler())
	if response.Code != http.StatusOK || len(audit.events) != 0 {
		t.Fatalf("routine read status = %d, events = %+v", response.Code, audit.events)
	}

	config.Scope, config.RequirePrivate = model.ServiceScopeAuditRead, true
	response = runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/admin/audit-events", serviceTokenOKHandler())
	if response.Code != http.StatusForbidden || len(audit.events) != 1 {
		t.Fatalf("denied status = %d, events = %+v", response.Code, audit.events)
	}
	denial := audit.events[0]
	if denial.Outcome != model.AuditOutcomeFailure || denial.Reason != serviceTokenDenialScope ||
		denial.PrincipalID != "principal-one" || denial.Action != serviceTokenAuditAction {
		t.Fatalf("denial = %+v", denial)
	}
}

func TestServiceTokenAuditRecordsMutationsAndHandlerFailures(t *testing.T) {
	audit := &recordedServiceTokenAudit{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipal(
			[]model.ServiceScope{model.ServiceScopeNodesWritePresentation}, nil)},
		Audit: audit, Scope: model.ServiceScopeNodesWritePresentation,
	}
	response := runAuditedServiceTokenRequest(t, config, http.MethodPut, "/api/v1/nodes/node-one", serviceTokenOKHandler())
	if response.Code != http.StatusOK || len(audit.events) != 1 || audit.events[0].Outcome != model.AuditOutcomeSuccess {
		t.Fatalf("mutation status = %d, events = %+v", response.Code, audit.events)
	}

	failing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	response = runAuditedServiceTokenRequest(t, config, http.MethodPut, "/api/v1/nodes/node-one", failing)
	if response.Code != http.StatusNotFound || len(audit.events) != 2 {
		t.Fatalf("failure status = %d, events = %+v", response.Code, audit.events)
	}
	if audit.events[1].Outcome != model.AuditOutcomeFailure || audit.events[1].Reason != serviceTokenAuditHandlerError {
		t.Fatalf("failure event = %+v", audit.events[1])
	}
}

func TestServiceTokenFailureAdmissionCapsAuditWritesForUnknownTokens(t *testing.T) {
	audit := &recordedServiceTokenAudit{}
	metrics := &recordedServiceTokenMetrics{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{err: errors.New("unknown token")},
		Audit:         audit, Metrics: metrics,
		FailureLimiter: serviceprincipal.NewLimiter(serviceprincipal.RateLimits{
			Token:  serviceprincipal.RateLimit{Burst: 1, Interval: time.Second},
			Source: serviceprincipal.RateLimit{Burst: 1, Interval: time.Second},
		}),
	}
	first := runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/nodes/manage", serviceTokenOKHandler())
	if first.Code != http.StatusUnauthorized || len(audit.events) != 1 {
		t.Fatalf("first attempt status = %d, events = %+v", first.Code, audit.events)
	}
	second := runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/nodes/manage", serviceTokenOKHandler())
	if second.Code != http.StatusTooManyRequests || len(audit.events) != 1 {
		t.Fatalf("second attempt status = %d, events = %+v", second.Code, audit.events)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited authentication is missing Retry-After")
	}
	if len(metrics.limits) != 1 || metrics.limits[0] != serviceprincipal.RateLimitScopeSource {
		t.Fatalf("rate limit metrics = %v", metrics.limits)
	}
	if len(metrics.outcomes) != 2 || metrics.outcomes[1] != serviceTokenOutcomeDenied {
		t.Fatalf("authentication outcomes = %v", metrics.outcomes)
	}
}

func TestServiceTokenFailureAdmissionPassesUnknownTokensWithoutLimiter(t *testing.T) {
	audit := &recordedServiceTokenAudit{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{err: serviceprincipal.ErrUnavailable},
		Audit:         audit, Scope: model.ServiceScopeNodesRead,
	}
	for attempt := 0; attempt < 3; attempt++ {
		response := runAuditedServiceTokenRequest(t, config, http.MethodGet, "/api/v1/nodes/manage", serviceTokenOKHandler())
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d status = %d", attempt, response.Code)
		}
	}
	if len(audit.events) != 3 || audit.events[0].Reason != serviceTokenAuditUnavailable {
		t.Fatalf("events = %+v", audit.events)
	}
}
