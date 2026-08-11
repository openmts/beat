package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestRouterAdministratorSecurityLifecycle(t *testing.T) {
	router := setupSessionRouter(t)
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate status = %d, body = %s", reauth.Code, reauth.Body.String())
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users",
		`{"username":"operator","display_name":"Operator","role":"admin",`+
			`"password":"operator password long"}`, cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create administrator status = %d, body = %s", created.Code, created.Body.String())
	}
	var user struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil || user.Data.ID == "" {
		t.Fatalf("decode administrator = %#v, %v", user, err)
	}
	listed := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/users", "", cookie, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "operator") {
		t.Fatalf("list administrators status = %d, body = %s", listed.Code, listed.Body.String())
	}
	updated := requestRouterWithCookie(router, http.MethodPut, "/api/v1/admin/users/"+user.Data.ID,
		`{"display_name":"Updated Operator","role":"admin","enabled":true}`,
		cookie, "http://example.com")
	if updated.Code != http.StatusOK {
		t.Fatalf("update administrator status = %d, body = %s", updated.Code, updated.Body.String())
	}

	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login",
		`{"username":"operator","password":"operator password long"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		t.Fatalf("administrator login status = %d, body = %s", login.Code, login.Body.String())
	}
	operatorCookie := login.Result().Cookies()[0]
	forbidden := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/users", "", operatorCookie, "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("administrator user-list status = %d", forbidden.Code)
	}
	sessions := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/sessions", "", cookie, "")
	if sessions.Code != http.StatusOK || !strings.Contains(sessions.Body.String(), `"current":true`) {
		t.Fatalf("sessions status = %d, body = %s", sessions.Code, sessions.Body.String())
	}
	revoked := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/sessions/others", "",
		cookie, "http://example.com")
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke other sessions status = %d, body = %s", revoked.Code, revoked.Body.String())
	}

	setup := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users/me/totp",
		`{"code":""}`, cookie, "http://example.com")
	if setup.Code != http.StatusOK {
		t.Fatalf("TOTP setup status = %d, body = %s", setup.Code, setup.Body.String())
	}
	var totpSetup struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &totpSetup); err != nil {
		t.Fatalf("decode TOTP setup: %v", err)
	}
	code, err := totp.GenerateCode(totpSetup.Data.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}
	enabled := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users/me/totp",
		`{"code":"`+code+`"}`, cookie, "http://example.com")
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable TOTP status = %d, body = %s", enabled.Code, enabled.Body.String())
	}
	code, _ = totp.GenerateCode(totpSetup.Data.Secret, time.Now().UTC())
	reauth = requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":"`+code+`"}`,
		cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("TOTP reauthenticate status = %d, body = %s", reauth.Code, reauth.Body.String())
	}
	disabled := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/users/me/totp", "",
		cookie, "http://example.com")
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable TOTP status = %d, body = %s", disabled.Code, disabled.Body.String())
	}
	changed := requestRouterWithCookie(router, http.MethodPut, "/api/v1/admin/users/me/password",
		`{"current_password":"correct horse battery staple",`+
			`"new_password":"replacement horse battery staple","totp_code":""}`,
		cookie, "http://example.com")
	if changed.Code != http.StatusOK {
		t.Fatalf("change password status = %d, body = %s", changed.Code, changed.Body.String())
	}
	deleted := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/users/"+user.Data.ID, "",
		cookie, "http://example.com")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete administrator status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	audit := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/audit-events?limit=20", "", cookie, "")
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "admin.mutation") {
		t.Fatalf("audit status = %d, body = %s", audit.Code, audit.Body.String())
	}
	logout := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/logout", "", cookie, "http://example.com")
	if logout.Code != http.StatusOK || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status = %d, cookies = %#v", logout.Code, logout.Result().Cookies())
	}
}

func TestRouterSessionAuthenticationTransition(t *testing.T) {
	router := setupSessionRouter(t)
	state := requestRouter(router, http.MethodGet, "/api/v1/auth/state", "", "", "")
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"setup_required":true`) {
		t.Fatalf("state status = %d, body = %s", state.Code, state.Body.String())
	}
	body := `{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",` +
		`"password":"correct horse battery staple"}`
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", body, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure {
		t.Fatalf("cookie = %#v", cookie)
	}
	bearer := requestRouter(router, http.MethodGet, "/api/v1/ssh-keys", "", "Bearer admin-secret", "")
	if bearer.Code != http.StatusUnauthorized {
		t.Fatalf("legacy bearer status after bootstrap = %d", bearer.Code)
	}
	session := requestRouterWithCookie(router, http.MethodGet, "/api/v1/auth/session", "", cookie, "")
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"username":"owner"`) {
		t.Fatalf("session status = %d, body = %s", session.Code, session.Body.String())
	}
	csrf := requestRouterWithCookie(router, http.MethodPost, "/api/v1/groups", `{"name":"ops"}`, cookie, "")
	if csrf.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", csrf.Code)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/groups", `{"name":"ops"}`,
		cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("same-origin status = %d, body = %s", created.Code, created.Body.String())
	}
}

func TestRouterSecureCookieAndHeaders(t *testing.T) {
	router := setupSessionRouter(t)
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/auth/bootstrap",
		strings.NewReader(`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`))
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !response.Result().Cookies()[0].Secure {
		t.Fatalf("status = %d, cookie = %#v", response.Code, response.Result().Cookies())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options",
		"Referrer-Policy", "Permissions-Policy", "Strict-Transport-Security"} {
		if response.Header().Get(header) == "" {
			t.Fatalf("header %s is missing", header)
		}
	}
}

func setupSessionRouter(t *testing.T) *Router {
	return setupSessionRouterWithOptions(t)
}

func setupSessionRouterWithOptions(t *testing.T, options ...RouterOption) *Router {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	randomData := make([]byte, 8192)
	for index := range randomData {
		randomData[index] = byte(index)
	}
	random := bytes.NewReader(randomData)
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"), random)
	if err != nil {
		t.Fatalf("new secret box: %v", err)
	}
	adminStore := store.NewAdminStore(sqlite.DB)
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: adminStore, Secrets: secrets, LegacyToken: "admin-secret", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	base := []RouterOption{WithAdminToken("admin-secret"), WithAdminSecurity(security)}
	return NewRouter(store.NewNodeStore(sqlite.DB), store.NewGroupStore(sqlite.DB),
		store.NewSSHKeyStore(sqlite.DB), store.NewAlertRuleStore(sqlite.DB),
		store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		append(base, options...)...)
}

func requestRouter(
	router *Router, method, path, body, authorization, origin string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}

func requestRouterWithCookie(
	router *Router, method, path, body string, cookie *http.Cookie, origin string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	request.AddCookie(cookie)
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}
