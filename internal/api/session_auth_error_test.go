package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestSessionAuthenticationStoreFailures(t *testing.T) {
	router := &Router{security: closedRouterSecurity(t), adminToken: "token"}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, middlewareHandler := range []func(http.Handler) http.Handler{
		router.sessionAdmin, router.sessionWebSocketAdmin,
	} {
		response := httptest.NewRecorder()
		middlewareHandler(next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("store failure status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	}
}

func TestCookiePrincipalAndAuditHelpers(t *testing.T) {
	router := setupSessionRouter(t)
	for _, cookie := range []*http.Cookie{nil, {Name: handler.AdminSessionCookieName},
		{Name: handler.AdminSessionCookieName, Value: "invalid"}} {
		request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		if _, ok := router.cookiePrincipal(response, request); ok || response.Code != http.StatusUnauthorized {
			t.Fatalf("cookie %#v authentication = %v, status = %d", cookie, ok, response.Code)
		}
	}
	getRequest := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	postRequest := httptest.NewRequest(http.MethodPost, "http://example.com/", nil)
	if auditAction(getRequest) != "admin.read" || auditAction(postRequest) != "admin.mutation" {
		t.Fatal("administrator audit action is incorrect")
	}
	getRequest.RemoteAddr = "local"
	if requestIP(getRequest) != "local" {
		t.Fatalf("request IP = %q, want local", requestIP(getRequest))
	}
}

func TestAuditMiddlewareBypassAndFailureOutcome(t *testing.T) {
	router := setupSessionRouter(t)
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "missing", Username: "missing"}}
	bypass := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/auth/session", nil)
	bypass.Pattern = "GET /api/v1/auth/session"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusBadRequest)
	})
	response := httptest.NewRecorder()
	router.auditAdminRequest(next, bypass, &principal).ServeHTTP(response, bypass)
	if !called || response.Code != http.StatusBadRequest {
		t.Fatalf("bypass called = %v, status = %d", called, response.Code)
	}
	request := httptest.NewRequest(http.MethodDelete, "http://example.com/api/v1/resource", nil)
	request.Pattern = "DELETE /api/v1/resource/{id}"
	response = httptest.NewRecorder()
	router.auditAdminRequest(next, request, &principal).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("audited failure status = %d", response.Code)
	}
}

func closedRouterSecurity(t *testing.T) *adminauth.Service {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("new router store: %v", err)
	}
	random := bytes.NewReader(bytes.Repeat([]byte{6}, 4096))
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"), random)
	if err != nil {
		t.Fatalf("new router secrets: %v", err)
	}
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, Random: random, Now: time.Now,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		_ = sqliteStore.Close()
		t.Fatalf("new router security: %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close router store: %v", err)
	}
	return security
}
