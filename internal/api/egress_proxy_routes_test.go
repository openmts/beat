package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/egressproxy"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func setupEgressProxyRouter(t *testing.T) *Router {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	secrets := testutil.NewSecrets(t, sqlite.DB)
	random := bytes.Repeat([]byte{17}, 8192)
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: store.NewAdminStore(sqlite.DB), Secrets: secrets, LegacyToken: "admin-secret",
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, bytes.NewReader(random)),
	})
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	proxy, err := egressproxy.NewService(store.NewEgressProxyStore(sqlite.DB), secrets)
	if err != nil {
		t.Fatalf("new egress proxy: %v", err)
	}
	return NewRouter(store.NewNodeStore(sqlite.DB), store.NewGroupStore(sqlite.DB),
		store.NewSSHKeyStore(sqlite.DB), store.NewAlertRuleStore(sqlite.DB),
		store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		WithAdminToken("admin-secret"), WithAdminSecurity(security), WithEgressProxy(proxy))
}

func TestEgressProxyRouteRequiresOwnerAndRecentAuthentication(t *testing.T) {
	router := setupEgressProxyRouter(t)
	const path = "/api/v1/admin/settings/egress-proxy"
	if response := requestRouter(router, http.MethodGet, path, "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", response.Code)
	}
	if response := requestRouter(router, http.MethodPost, path+"/test", "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous probe = %d", response.Code)
	}
	owner := boundaryOwner(t, router)
	if _, err := router.security.CreateUser(t.Context(), &owner.Principal, adminauth.CreateUserRequest{
		Username: "operator", DisplayName: "Operator", Role: model.AdminRoleAdmin, Password: "operator password long",
	}); err != nil {
		t.Fatal(err)
	}
	admin, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: "operator", Password: "operator password long"})
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: handler.AdminSessionCookieName, Value: admin.Token}
	if response := requestRouterWithCookie(router, http.MethodGet, path, "", adminCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("admin = %d", response.Code)
	}
	if response := requestRouterWithCookie(router, http.MethodPost, path+"/test", "", adminCookie, "http://example.com"); response.Code != http.StatusForbidden {
		t.Fatalf("admin probe = %d", response.Code)
	}
	ownerCookie := &http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token}
	if response := requestRouterWithCookie(router, http.MethodGet, path, "", ownerCookie, ""); response.Code != http.StatusOK {
		t.Fatalf("owner get = %d %s", response.Code, response.Body.String())
	}
	if response := requestRouterWithCookie(router, http.MethodPost, path+"/test", "", ownerCookie, "http://example.com"); response.Code != http.StatusConflict {
		t.Fatalf("disabled probe = %d %s", response.Code, response.Body.String())
	}
	password := "proxy-password-that-must-never-leak"
	body, _ := json.Marshal(egressproxy.Update{Enabled: true, ProxyURL: "http://127.0.0.1:7890", Username: "proxy", Password: &password, ExpectedRevision: 1})
	response := requestRouterWithCookie(router, http.MethodPut, path, string(body), ownerCookie, "http://example.com")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), password) || !strings.Contains(response.Body.String(), `"has_password":true`) {
		t.Fatalf("owner update = %d %s", response.Code, response.Body.String())
	}
	fresh, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: "owner", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	response = requestRouterWithCookie(router, http.MethodPut, path, string(body), &http.Cookie{Name: handler.AdminSessionCookieName, Value: fresh.Token}, "http://example.com")
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("fresh login update = %d %s", response.Code, response.Body.String())
	}
	response = requestRouterWithCookie(router, http.MethodPost, path+"/test", "", &http.Cookie{Name: handler.AdminSessionCookieName, Value: fresh.Token}, "http://example.com")
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("fresh login probe = %d %s", response.Code, response.Body.String())
	}
}
