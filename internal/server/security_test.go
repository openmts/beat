package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"beat/internal/auth"
	"beat/internal/config"
	"beat/internal/platform/sqlite"
)

func newSecurityTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		cfg:          config.Config{Listen: "[::]:9099", EnrollmentTokenTTL: 10 * time.Minute},
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:        store,
		authManager:  manager,
		loginLimiter: newLoginLimiter(),
	}
	return server, server.routes()
}

func TestCSRFOriginHeadersAndPersistentSession(t *testing.T) {
	server, router := newSecurityTestServer(t)
	session, err := server.authManager.IssueSession(auth.DefaultUsername, 1)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminSessionCookie + "=" + session.Token

	response := serveRequest(router, http.MethodGet, "/api/v1/meta", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("meta status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("security headers missing: %+v", response.Header())
	}

	response = serveAdminRequest(router, http.MethodPost, "/api/v1/groups", cookie, "", `{"name":"test"}`)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_invalid") {
		t.Fatalf("mutation without CSRF = %d: %s", response.Code, response.Body.String())
	}
	response = serveAdminRequest(router, http.MethodPost, "/api/v1/groups", cookie, session.CSRFToken, `{"name":""}`)
	if response.Code == http.StatusForbidden {
		t.Fatalf("valid CSRF token was rejected: %s", response.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(`{"name":"test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", cookie)
	request.Header.Set(csrfHeader, session.CSRFToken)
	request.Header.Set("Origin", "https://attacker.example")
	crossOrigin := httptest.NewRecorder()
	router.ServeHTTP(crossOrigin, request)
	if crossOrigin.Code != http.StatusForbidden || !strings.Contains(crossOrigin.Body.String(), "cross_origin_rejected") {
		t.Fatalf("cross-origin mutation = %d: %s", crossOrigin.Code, crossOrigin.Body.String())
	}

	secondManager, err := auth.NewManager(context.Background(), server.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, valid, err := secondManager.ValidateSession(context.Background(), session.Token); err != nil || !valid {
		t.Fatalf("session did not survive manager restart: valid=%v err=%v", valid, err)
	}
}

func TestLoginRateLimitAndAudit(t *testing.T) {
	server, router := newSecurityTestServer(t)
	for attempt := 1; attempt <= 5; attempt++ {
		response := serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"wrong"}`)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d: %s", attempt, response.Code, response.Body.String())
		}
	}
	response := serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"wrong"}`)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limited login = %d headers=%+v: %s", response.Code, response.Header(), response.Body.String())
	}
	count, err := server.store.CountAudit(context.Background(), "admin_login_failed")
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("login failure audit count = %d, want 5", count)
	}
}
