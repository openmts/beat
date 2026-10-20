package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

func TestOwnerWrappersRejectMissingRoleAndRecentProof(t *testing.T) {
	router := setupSessionRouter(t)
	owner := boundaryOwner(t, router)
	const password = "correct horse battery staple"
	if _, err := router.security.CreateUser(t.Context(), &owner.Principal, adminauth.CreateUserRequest{
		Username: "operator", DisplayName: "Operator", Role: model.AdminRoleAdmin, Password: password}); err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"owner", "operator"} {
		login, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: username, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://example.com/private", nil)
		request.Header.Set("Origin", "http://example.com")
		request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: login.Token})
		response := httptest.NewRecorder()
		router.recentAuthOwnerAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("unauthorized owner action ran")
		})).ServeHTTP(response, request)
		expected := http.StatusForbidden
		if username == "owner" {
			expected = http.StatusPreconditionRequired
		}
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("role/proof check: want %d private response, got %d", expected, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ownerAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("anonymous action ran") })).ServeHTTP(
		response, httptest.NewRequest(http.MethodGet, "http://example.com/private", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous owner request = %d", response.Code)
	}
}

func TestSessionWebSocketBoundaryKeepsOriginAndNoStore(t *testing.T) {
	router := setupSessionRouter(t)
	owner := boundaryOwner(t, router)
	for _, origin := range []string{"", "http://other.example.com", "http://example.com"} {
		request := httptest.NewRequest(http.MethodGet, "http://example.com/private", nil)
		request.Header.Set("Origin", origin)
		request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token})
		response := httptest.NewRecorder()
		router.sessionWebSocketAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(response, request)
		expected := http.StatusForbidden
		if origin == "http://example.com" {
			expected = http.StatusNoContent
		}
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("websocket boundary = %d", response.Code)
		}
	}
}
