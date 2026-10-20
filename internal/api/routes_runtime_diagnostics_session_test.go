package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/runtimediag"
)

// runtimeDiagnosticsProbe 只记录会话撤销，用于验证登出后的捕获清理接线。
type runtimeDiagnosticsProbe struct{ revoked []string }

func (probe *runtimeDiagnosticsProbe) Summary() runtimediag.Summary { return runtimediag.Summary{} }

func (probe *runtimeDiagnosticsProbe) List(string) []runtimediag.Capture { return nil }

func (probe *runtimeDiagnosticsProbe) Busy() bool { return false }

func (probe *runtimeDiagnosticsProbe) Start(context.Context, string, string, string,
	time.Duration) (runtimediag.Capture, error) {
	return runtimediag.Capture{}, nil
}

func (probe *runtimeDiagnosticsProbe) Download(string, string) (io.ReadCloser, runtimediag.Capture, error) {
	return nil, runtimediag.Capture{}, nil
}

func (probe *runtimeDiagnosticsProbe) Cancel(string, string) error { return nil }

func (probe *runtimeDiagnosticsProbe) RevokeSession(id string) {
	probe.revoked = append(probe.revoked, id)
}

func runtimeOwnerPrincipal(t *testing.T, router *Router) adminauth.LoginResult {
	t.Helper()
	owner, err := router.security.Bootstrap(t.Context(), adminauth.BootstrapRequest{
		BootstrapToken: "admin-secret", Username: "owner", DisplayName: "Owner",
		Password: "correct horse battery staple"})
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	return owner
}

func runtimeRequestWithPrincipal(principal adminauth.LoginResult) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/captures", nil)
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal.Principal))
}

func TestRuntimeSessionCleanupPassesThroughWithoutService(t *testing.T) {
	router := setupSessionRouterWithOptions(t)
	next := http.NewServeMux()
	if router.runtimeSessionCleanup(next, runtimePathSession) != http.Handler(next) {
		t.Fatal("cleanup wrapper was installed without a runtime diagnostics service")
	}
	if router.runtimeDiagnostics != nil {
		t.Fatal("runtime diagnostics service was configured unexpectedly")
	}
}

func TestRuntimeSessionCleanupRevokesSessionAfterRequest(t *testing.T) {
	probe := &runtimeDiagnosticsProbe{}
	router := setupSessionRouterWithOptions(t, WithRuntimeDiagnostics(probe, nil))
	owner := runtimeOwnerPrincipal(t, router)
	served := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })
	request := runtimeRequestWithPrincipal(owner)
	router.runtimeSessionCleanup(next, runtimeCurrentSession).ServeHTTP(httptest.NewRecorder(), request)
	if !served || len(probe.revoked) != 1 || probe.revoked[0] != owner.Principal.Session.ID {
		t.Fatalf("served = %v revoked = %v", served, probe.revoked)
	}
	probe.revoked = nil
	router.runtimeSessionCleanup(next, func(*http.Request) []string { return []string{"", "session-9"} }).
		ServeHTTP(httptest.NewRecorder(), request)
	if len(probe.revoked) != 1 || probe.revoked[0] != "session-9" {
		t.Fatalf("blank session id was forwarded: %v", probe.revoked)
	}
}

func TestRuntimeSessionHelpersReadRequestState(t *testing.T) {
	anonymous := httptest.NewRequest(http.MethodGet, "/", nil)
	if ids := runtimeCurrentSession(anonymous); ids != nil {
		t.Fatalf("anonymous session ids = %v", ids)
	}
	owner := runtimeOwnerPrincipal(t, setupSessionRouterWithOptions(t))
	ids := runtimeCurrentSession(runtimeRequestWithPrincipal(owner))
	if len(ids) != 1 || ids[0] != owner.Principal.Session.ID {
		t.Fatalf("session ids = %v", ids)
	}
	pathRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	pathRequest.SetPathValue("id", "session-from-path")
	if ids := runtimePathSession(pathRequest); len(ids) != 1 || ids[0] != "session-from-path" {
		t.Fatalf("path session ids = %v", ids)
	}
}

func TestRuntimeOtherSessionsSkipsCurrentSession(t *testing.T) {
	router := setupSessionRouterWithOptions(t)
	owner := runtimeOwnerPrincipal(t, router)
	second, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: "owner",
		Password: "correct horse battery staple", IPAddress: "127.0.0.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	ids := router.runtimeOtherSessions(runtimeRequestWithPrincipal(owner))
	if len(ids) != 1 || ids[0] != second.Principal.Session.ID {
		t.Fatalf("other sessions = %v", ids)
	}
	if ids := router.runtimeOtherSessions(httptest.NewRequest(http.MethodGet, "/", nil)); ids != nil {
		t.Fatalf("anonymous other sessions = %v", ids)
	}
	if ids := (&Router{}).runtimeOtherSessions(runtimeRequestWithPrincipal(owner)); ids != nil {
		t.Fatalf("service-less other sessions = %v", ids)
	}
}
