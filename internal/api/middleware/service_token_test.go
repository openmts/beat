package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
)

type stubServiceTokenAuthenticator struct {
	principal *model.ServicePrincipal
	err       error
	token     string
	source    netip.Addr
}

func (stub *stubServiceTokenAuthenticator) Authenticate(_ context.Context, token string, source netip.Addr) (*model.ServicePrincipal, error) {
	stub.token, stub.source = token, source
	return stub.principal, stub.err
}

type recordedServiceTokenMetrics struct {
	outcomes  []string
	denials   []string
	limits    []string
	latencies []string
	durations []time.Duration
}

func (metrics *recordedServiceTokenMetrics) ObserveServiceTokenAuth(outcome string) {
	metrics.outcomes = append(metrics.outcomes, outcome)
}

func (metrics *recordedServiceTokenMetrics) ObserveServiceTokenDenial(reason string) {
	metrics.denials = append(metrics.denials, reason)
}

func (metrics *recordedServiceTokenMetrics) ObserveServiceTokenRateLimit(scope string) {
	metrics.limits = append(metrics.limits, scope)
}

func (metrics *recordedServiceTokenMetrics) ObserveServiceTokenLatency(scope string, duration time.Duration) {
	metrics.latencies = append(metrics.latencies, scope)
	metrics.durations = append(metrics.durations, duration)
}

func serviceTokenPrincipal(scopes []model.ServiceScope, resources []model.ServiceResource) *model.ServicePrincipal {
	return &model.ServicePrincipal{
		ID: "principal-one", Name: "nightly", TokenPrefix: "beat_service_v1_abcdefgh",
		Scopes: scopes, Resources: resources, ExpiresAt: time.Now().Add(time.Hour),
	}
}

func runServiceTokenAccess(t *testing.T, config ServiceTokenAccessConfig, header string, target string) *httptest.ResponseRecorder {
	t.Helper()
	return runServiceTokenAccessRequest(t, config, header, target, nil)
}

func runServiceTokenAccessRequest(t *testing.T, config ServiceTokenAccessConfig, header string, target string, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	if config.Scope == "" {
		config.Scope = model.ServiceScopeNodesRead
	}
	handler := ServiceTokenAccess(config)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, ok := ServicePrincipalFrom(request.Context())
		if !ok {
			WriteAuthError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(principal.ID))
	}))
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.RemoteAddr = "192.0.2.10:1234"
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}
	if header != "" {
		request.Header.Set("Authorization", header)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestServiceTokenAccessFallsBackWithoutServiceCredential(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	fallback := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			WriteAuthError(w, http.StatusUnauthorized, "session required")
		})
	}
	config := ServiceTokenAccessConfig{Authenticator: &stubServiceTokenAuthenticator{}, Metrics: metrics, Browser: fallback}

	for name, header := range map[string]string{"missing": "", "other type": "Bearer beat-agent-token", "blank bearer": "Bearer "} {
		response := runServiceTokenAccess(t, config, header, "/api/v1/alerts/events")
		if response.Code != http.StatusUnauthorized || response.Body.String() == "" {
			t.Fatalf("%s response = %d %q, want fallback 401", name, response.Code, response.Body.String())
		}
	}
	if len(metrics.outcomes) != 0 || len(metrics.denials) != 0 {
		t.Fatalf("metrics recorded for fallback: %+v", metrics)
	}
}

func TestServiceTokenAccessServesPermittedRequest(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{principal: serviceTokenPrincipal([]model.ServiceScope{model.ServiceScopeAlertsRead}, nil)}
	metrics := &recordedServiceTokenMetrics{}
	config := ServiceTokenAccessConfig{Authenticator: authenticator, Metrics: metrics, Scope: model.ServiceScopeAlertsRead}

	response := runServiceTokenAccess(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if response.Code != http.StatusOK || response.Body.String() != "principal-one" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if authenticator.token != "beat_service_v1_abcdefghijklmnop" || authenticator.source != netip.MustParseAddr("192.0.2.10") {
		t.Fatalf("authenticator saw token %q source %s", authenticator.token, authenticator.source)
	}
	if len(metrics.outcomes) != 1 || metrics.outcomes[0] != "authenticated" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}
}

func TestServiceTokenAccessDeniesMissingScope(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipal([]model.ServiceScope{model.ServiceScopeHealthRead}, nil)},
		Metrics:       metrics, Scope: model.ServiceScopeAlertsRead,
	}
	response := runServiceTokenAccess(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if len(metrics.denials) != 1 || metrics.denials[0] != "scope" {
		t.Fatalf("denials = %v", metrics.denials)
	}
}

func TestServiceTokenAccessDeniesResourceOutsideConstraint(t *testing.T) {
	principal := serviceTokenPrincipal([]model.ServiceScope{model.ServiceScopeMetricsRead},
		[]model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "node-one"}})
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: principal}, Metrics: &recordedServiceTokenMetrics{},
		Scope: model.ServiceScopeMetricsRead, ResourceKind: model.ServiceResourceNode,
		ResolveResource: ServicePathResource("id"),
	}
	denied := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-two/metrics", map[string]string{"id": "node-two"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", denied.Code)
	}
	allowed := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-one/metrics", map[string]string{"id": "node-one"})
	if allowed.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", allowed.Code)
	}
}

func TestServiceTokenAccessReportsAuthenticationFailures(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	denied := runServiceTokenAccess(t, ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{err: serviceprincipal.ErrUnauthorized}, Metrics: metrics,
	}, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", denied.Code)
	}
	unavailable := runServiceTokenAccess(t, ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{err: serviceprincipal.ErrUnavailable}, Metrics: metrics,
	}, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", unavailable.Code)
	}
	if len(metrics.outcomes) != 2 || metrics.outcomes[0] != "denied" || metrics.outcomes[1] != "unavailable" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}
}

func TestServiceTokenAccessRateLimitsPerToken(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	limiter := serviceprincipal.NewLimiter(serviceprincipal.RateLimits{
		Token:  serviceprincipal.RateLimit{Burst: 1, Interval: time.Minute},
		Source: serviceprincipal.RateLimit{Burst: 10, Interval: time.Minute},
	})
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipal([]model.ServiceScope{model.ServiceScopeAlertsRead}, nil)},
		Limiter:       limiter, Metrics: metrics, Scope: model.ServiceScopeAlertsRead,
	}
	if response := runServiceTokenAccess(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events"); response.Code != http.StatusOK {
		t.Fatalf("first status = %d", response.Code)
	}
	limited := runServiceTokenAccess(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", limited.Code)
	}
	if limited.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", limited.Header().Get("Retry-After"))
	}
	if len(metrics.limits) != 1 || metrics.limits[0] != serviceprincipal.RateLimitScopeToken {
		t.Fatalf("limits = %v", metrics.limits)
	}
}

func TestRetryAfterSecondsFloorsAtOne(t *testing.T) {
	if seconds := retryAfterSeconds(0); seconds != 1 {
		t.Fatalf("seconds = %d, want 1", seconds)
	}
	if seconds := retryAfterSeconds(1500 * time.Millisecond); seconds != 2 {
		t.Fatalf("seconds = %d, want 2", seconds)
	}
}

func TestServiceTokenAccessWithoutAuthenticatorUsesFallback(t *testing.T) {
	called := false
	handler := ServiceTokenAccess(ServiceTokenAccessConfig{Browser: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			called = true
			next.ServeHTTP(w, request)
		})
	}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/events", nil)
	request.Header.Set("Authorization", "Bearer beat_service_v1_abcdefghijklmnop")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("called = %v status = %d", called, response.Code)
	}
}

var errServiceTokenTest = errors.New("service token test failure")

func TestServiceTokenAccessTreatsUnknownErrorsAsUnauthorized(t *testing.T) {
	response := runServiceTokenAccess(t, ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{err: errServiceTokenTest},
	}, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func serviceTokenPrincipalWithVisibility(
	visibility model.ServiceVisibility, scopes []model.ServiceScope, resources []model.ServiceResource,
) *model.ServicePrincipal {
	principal := serviceTokenPrincipal(scopes, resources)
	principal.Visibility = visibility
	return principal
}

func TestServiceTokenAccessRejectsPublicTokenOnPrivateEndpoints(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	config := ServiceTokenAccessConfig{
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipalWithVisibility(
			model.ServiceVisibilityPublic, []model.ServiceScope{model.ServiceScopeAlertsRead}, nil)},
		Metrics: metrics, Scope: model.ServiceScopeAlertsRead, RequirePrivate: true,
	}
	response := runServiceTokenAccess(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if response.Code != http.StatusForbidden {
		t.Fatalf("public visibility status = %d, want 403", response.Code)
	}
	if len(metrics.denials) != 1 || metrics.denials[0] != "visibility" {
		t.Fatalf("denials = %v", metrics.denials)
	}

	privateMetrics := &recordedServiceTokenMetrics{}
	privateConfig := config
	privateConfig.Metrics = privateMetrics
	privateConfig.Authenticator = &stubServiceTokenAuthenticator{principal: serviceTokenPrincipalWithVisibility(
		model.ServiceVisibilityPrivate, []model.ServiceScope{model.ServiceScopeAlertsRead}, nil)}
	allowed := runServiceTokenAccess(t, privateConfig, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/alerts/events")
	if allowed.Code != http.StatusOK || len(privateMetrics.denials) != 0 {
		t.Fatalf("private visibility response = %d, denials = %v", allowed.Code, privateMetrics.denials)
	}
}

func TestServiceTokenAccessChecksResourceVisibility(t *testing.T) {
	metrics := &recordedServiceTokenMetrics{}
	resolver := func(_ context.Context, kind model.ServiceResourceKind, id string) (bool, error) {
		switch {
		case kind != model.ServiceResourceNode:
			return false, nil
		case id == "node-public":
			return true, nil
		case id == "node-error":
			return false, errors.New("visibility lookup failed")
		default:
			return false, nil
		}
	}
	config := ServiceTokenAccessConfig{
		// 可见性判定发生在资源约束之后；本用例用无节点约束的令牌确保能进入解析器。
		Authenticator: &stubServiceTokenAuthenticator{principal: serviceTokenPrincipalWithVisibility(
			model.ServiceVisibilityPublic, []model.ServiceScope{model.ServiceScopeMetricsRead}, nil)},
		Metrics: metrics, Scope: model.ServiceScopeMetricsRead, ResourceKind: model.ServiceResourceNode,
		ResolveResource:    func(request *http.Request) string { return request.PathValue("id") },
		ResourceVisibility: resolver,
	}

	public := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-public/metrics",
		map[string]string{"id": "node-public"})
	if public.Code != http.StatusOK {
		t.Fatalf("public resource status = %d, body = %q", public.Code, public.Body.String())
	}

	hidden := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-hidden/metrics",
		map[string]string{"id": "node-hidden"})
	if hidden.Code != http.StatusForbidden || len(metrics.denials) != 1 || metrics.denials[0] != "visibility" {
		t.Fatalf("hidden resource response = %d, denials = %v", hidden.Code, metrics.denials)
	}

	unresolved := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes//metrics", nil)
	if unresolved.Code != http.StatusForbidden || len(metrics.denials) != 2 || metrics.denials[1] != "visibility" {
		t.Fatalf("unresolved resource response = %d, denials = %v", unresolved.Code, metrics.denials)
	}

	failing := runServiceTokenAccessRequest(t, config, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-error/metrics",
		map[string]string{"id": "node-error"})
	if failing.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed lookup status = %d", failing.Code)
	}
	// 认证先于可见性：失败查询在 authenticated 之后补记 unavailable。
	if len(metrics.outcomes) != 5 || metrics.outcomes[4] != "unavailable" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}

	// 资源约束先于可见性：带节点约束的令牌访问未授权节点由资源约束拒绝。
	constrainedMetrics := &recordedServiceTokenMetrics{}
	constrained := config
	constrained.Metrics = constrainedMetrics
	constrained.Authenticator = &stubServiceTokenAuthenticator{principal: serviceTokenPrincipalWithVisibility(
		model.ServiceVisibilityPublic, []model.ServiceScope{model.ServiceScopeMetricsRead},
		[]model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "node-public"}})}
	denied := runServiceTokenAccessRequest(t, constrained, "Bearer beat_service_v1_abcdefghijklmnop", "/api/v1/nodes/node-hidden/metrics",
		map[string]string{"id": "node-hidden"})
	if denied.Code != http.StatusForbidden || len(constrainedMetrics.denials) != 1 || constrainedMetrics.denials[0] != "resource" {
		t.Fatalf("constrained resource response = %d, denials = %v", denied.Code, constrainedMetrics.denials)
	}
}
