package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestAuthenticationAuditUsesExactHTTPRequestAttribution(t *testing.T) {
	security := auditHandlerSecurity(t)
	handler := NewAdminAuthHandler(security)
	bootstrap := newAdminRequest(t, nil, http.MethodPost, "/api/v1/auth/bootstrap", `{"bootstrap_token":"bootstrap","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`)
	first := auditContextCall(handler.HandleBootstrap, bootstrap)
	if first.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", first.Code)
	}
	principal, err := security.Authenticate(t.Context(), first.Result().Cookies()[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	reauth := newAdminRequest(t, &principal, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`)
	proof := auditContextCall(handler.HandleReauthenticate, reauth)
	logout := auditContextCall(handler.HandleLogout, newAdminRequest(t, &principal, http.MethodPost, "/api/v1/auth/logout", ""))
	login := auditContextCall(handler.HandleLogin, newAdminRequest(t, nil, http.MethodPost, "/api/v1/auth/login", `{"username":"owner","password":"correct horse battery staple"}`))
	for _, response := range []*httptest.ResponseRecorder{proof, logout, login} {
		if response.Code != http.StatusOK {
			t.Fatalf("authentication status %d", response.Code)
		}
	}
	page, err := security.ListAuditEvents(t.Context(), &principal, model.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"auth.bootstrap": first.Header().Get("X-Request-ID"), "auth.reauthenticate": proof.Header().Get("X-Request-ID"), "auth.logout": logout.Header().Get("X-Request-ID"), "auth.login": login.Header().Get("X-Request-ID")}
	for _, event := range page.Events {
		id, exists := ids[event.Action]
		if !exists {
			continue
		}
		if id == "" || event.RequestID != id || event.IPAddress != "192.0.2.50" || event.UserAgent != "audit-attribution-test" || event.ActorID != principal.User.ID {
			t.Fatalf("incorrect audit attribution for %s", event.Action)
		}
		delete(ids, event.Action)
	}
	if len(ids) != 0 {
		t.Fatal("missing authentication audit events")
	}
}

func auditContextCall(handle http.HandlerFunc, request *http.Request) *httptest.ResponseRecorder {
	request.RemoteAddr = "192.0.2.50:1234"
	request.Header.Set("User-Agent", "audit-attribution-test")
	response := httptest.NewRecorder()
	middleware.RequestContext(middleware.TrustedProxies{})(handle).ServeHTTP(response, request)
	return response
}

func auditHandlerSecurity(t *testing.T) *adminauth.Service {
	t.Helper()
	database, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: testutil.NewSecrets(t, database.DB), LegacyToken: "bootstrap", Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	return security
}
