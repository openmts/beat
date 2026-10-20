package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollouthttp"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func TestRolloutBudgetBoundsRatesKeysAndRelease(t *testing.T) {
	now := time.Now()
	budget := newRolloutBudget(rolloutBudgetPolicy{burst: 1, rate: 1, globalBurst: 1, globalRate: 1, concurrent: 1})
	release, ok := budget.acquire("node", now)
	if !ok {
		t.Fatal("first request rejected")
	}
	if _, ok := budget.acquire("other", now); ok {
		t.Fatal("concurrency not bounded")
	}
	release()
	release()
	if _, ok := budget.acquire("node", now.Add(-time.Hour)); ok {
		t.Fatal("clock rollback refilled budget")
	}
	if _, ok := budget.acquire("other", now); ok {
		t.Fatal("global burst not bounded")
	}
	release, ok = budget.acquire("node", now.Add(time.Second))
	if !ok {
		t.Fatal("budget did not refill")
	}
	release()
	for index := range rolloutMaximumKeys - 1 {
		budget.entries[fmt.Sprint(index)] = rolloutBucket{at: now, last: now}
	}
	if _, ok := budget.acquire("new", now.Add(2*time.Second)); ok {
		t.Fatal("unbounded key map")
	}
	release, ok = budget.acquire("new", now.Add(rolloutKeyRetention+time.Second))
	if !ok {
		t.Fatal("idle keys were not pruned")
	}
	release()
	if budget.active != 0 || len(budget.entries) != 1 {
		t.Fatal(budget.active, len(budget.entries))
	}
}

func TestRolloutAdmissionBoundsBodyAndCanonicalAddress(t *testing.T) {
	admission := newRolloutAdmission()
	called := 0
	next := admission.incomingGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called++; w.WriteHeader(204) }))
	for _, input := range []struct {
		ip, pattern string
		size        int64
		status      int
	}{{"invalid", "", 0, 400}, {"192.0.2.1:1234", "", agentrollouthttp.MaximumJSONBytes + 1, 413}, {"192.0.2.1:1234", rolloutUploadPattern, agentrollouthttp.MaximumUploadBytes + 1, 413}, {"[::ffff:192.0.2.1]:1234", "", 0, 204}} {
		request := httptest.NewRequest("POST", "/", nil)
		request.RemoteAddr = input.ip
		request.Pattern = input.pattern
		request.ContentLength = input.size
		response := httptest.NewRecorder()
		next.ServeHTTP(response, request)
		if response.Code != input.status || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, input.status)
		}
	}
	if called != 1 {
		t.Fatal(called)
	}
	if _, exists := admission.incoming.entries["192.0.2.1"]; !exists {
		t.Fatal("mapped address was not canonicalized")
	}
}

func TestRolloutIdentityBudgetAndPanicStayBounded(t *testing.T) {
	admission := newRolloutAdmission()
	admission.owners.policy.burst = 1
	admission.owners.policy.rate = 0
	request := httptest.NewRequest("GET", "/", nil)
	protected := admission.identityGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("sensitive-body-nonce") }), false)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{Session: model.AdminSession{ID: "owner-session"}}))
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != 500 || admission.owners.active != 0 {
		t.Fatal(response.Code)
	}
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != 429 || response.Header().Get("Retry-After") != "1" {
		t.Fatal(response.Code, response.Header())
	}
	request = request.WithContext(middleware.WithAgentIdentity(request.Context(), model.AgentIdentity{NodeID: "node", Mode: model.AgentCredentialLegacy}))
	if _, _, ok := admission.identity(request, true); ok {
		t.Fatal("legacy identity accepted")
	}
}

func TestRolloutRegistrationMissingServicesNeverExposesRoutes(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, nil, nil, nil, WithAgentRollouts(nil))
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/admin/agent-rollout-status", nil))
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
	for _, protect := range []func(http.Handler) http.Handler{func(next http.Handler) http.Handler { return router.rolloutOwner(next, false) }, router.rolloutAgent} {
		response := httptest.NewRecorder()
		protect(http.NotFoundHandler()).ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
		if response.Code != 503 {
			t.Fatal(response.Code)
		}
	}
}

func TestRolloutAdmissionUsesOnlyTrustedForwardedAddresses(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		admission := newRolloutAdmission()
		admission.incoming.policy.burst = 1
		admission.incoming.policy.rate = 0
		cidrs := ""
		if trusted {
			cidrs = "192.0.2.0/24"
		}
		proxies, err := middleware.ParseTrustedProxies(cidrs)
		if err != nil {
			t.Fatal(err)
		}
		protected := middleware.RequestContext(proxies)(admission.incomingGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })))
		for index := range 2 {
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", index+1))
			response := httptest.NewRecorder()
			protected.ServeHTTP(response, request)
			expected := 204
			if index == 1 && !trusted {
				expected = 429
			}
			if response.Code != expected {
				t.Fatal(trusted, index, response.Code)
			}
		}
	}
}

type rolloutReadDeadlineFailure struct{ *httptest.ResponseRecorder }

func (rolloutReadDeadlineFailure) SetReadDeadline(time.Time) error {
	return errors.New("closed connection")
}

func TestRolloutAdmissionRejectsActualDeadlineFailures(t *testing.T) {
	admission := newRolloutAdmission()
	called := false
	next := admission.incomingGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	response := rolloutReadDeadlineFailure{httptest.NewRecorder()}
	next.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 503 || called || admission.incoming.active != 0 {
		t.Fatal(response.Code, called, admission.incoming.active)
	}
}
