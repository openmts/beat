package server

import (
	"context"
	"encoding/json"
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

func TestAdminAuthenticationRoutes(t *testing.T) {
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
		cfg:         config.Config{Listen: ":9099", EnrollmentTokenTTL: 10 * time.Minute},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:       store,
		authManager: manager,
	}
	router := server.routes()

	response := serveRequest(router, http.MethodGet, "/api/v1/meta", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("public meta status = %d, want 200: %s", response.Code, response.Body.String())
	}
	response = serveRequest(router, http.MethodGet, "/api/v1/settings", "", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated settings status = %d, want 401: %s", response.Code, response.Body.String())
	}
	response = serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"wrong"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401: %s", response.Code, response.Body.String())
	}

	response = serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %s", response.Code, response.Body.String())
	}
	loginCookie := response.Result().Cookies()
	if len(loginCookie) != 1 || loginCookie[0].Name != adminSessionCookie || !loginCookie[0].HttpOnly {
		t.Fatalf("unexpected login cookies: %+v", loginCookie)
	}
	oldCookie := loginCookie[0].Name + "=" + loginCookie[0].Value
	var loginPayload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &loginPayload); err != nil || loginPayload.CSRFToken == "" {
		t.Fatalf("login did not return a CSRF token: %v %s", err, response.Body.String())
	}
	csrfToken := loginPayload.CSRFToken

	response = serveRequest(router, http.MethodGet, "/api/v1/auth/me", oldCookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":true`) {
		t.Fatalf("authenticated me response = %d %s", response.Code, response.Body.String())
	}

	response = serveAdminRequest(router, http.MethodPut, "/api/v1/auth/credentials", oldCookie, csrfToken, `{"currentPassword":"admin","username":"operator","newPassword":"a-new-secret"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("update credentials status = %d, want 200: %s", response.Code, response.Body.String())
	}
	newCookies := response.Result().Cookies()
	if len(newCookies) != 1 {
		t.Fatalf("expected a rotated session cookie, got %+v", newCookies)
	}
	newCookie := newCookies[0].Name + "=" + newCookies[0].Value
	var updatedPayload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updatedPayload); err != nil || updatedPayload.CSRFToken == "" {
		t.Fatalf("credential update did not rotate the CSRF token: %v %s", err, response.Body.String())
	}
	csrfToken = updatedPayload.CSRFToken

	response = serveAdminRequest(router, http.MethodPut, "/api/v1/auth/credentials", newCookie, "", `{"currentPassword":"a-new-secret","username":"operator","newPassword":"another-secret"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("credential update without CSRF = %d, want 403: %s", response.Code, response.Body.String())
	}
	response = serveRequest(router, http.MethodGet, "/api/v1/auth/me", oldCookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":false`) {
		t.Fatalf("old session remained valid: %d %s", response.Code, response.Body.String())
	}
	response = serveRequest(router, http.MethodGet, "/api/v1/auth/me", newCookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":true`) {
		t.Fatalf("rotated session is invalid: %d %s", response.Code, response.Body.String())
	}

	if _, err := auth.ResetCredentials(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	response = serveRequest(router, http.MethodGet, "/api/v1/auth/me", newCookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":false`) {
		t.Fatalf("session remained valid after credential reset: %d %s", response.Code, response.Body.String())
	}
	response = serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("default login after reset status = %d: %s", response.Code, response.Body.String())
	}
}

func serveRequest(handler http.Handler, method, path, cookie, body string) *httptest.ResponseRecorder {
	return serveAdminRequest(handler, method, path, cookie, "", body)
}

func serveAdminRequest(handler http.Handler, method, path, cookie, csrfToken, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	if csrfToken != "" {
		request.Header.Set(csrfHeader, csrfToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
