package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

func boundaryOwner(t *testing.T, router *Router) adminauth.LoginResult {
	t.Helper()
	const password = "correct horse battery staple"
	result, err := router.security.Bootstrap(t.Context(), adminauth.BootstrapRequest{
		BootstrapToken: "admin-secret", Username: "owner", DisplayName: "Owner", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.security.Reauthenticate(t.Context(), &result.Principal, password, ""); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOwnerWrappersEnforceOriginAndPrivateResponses(t *testing.T) {
	router := setupSessionRouter(t)
	owner := boundaryOwner(t, router)
	for name, wrap := range map[string]func(http.Handler) http.Handler{"owner": router.ownerAdmin, "recent": router.recentAuthOwnerAdmin} {
		t.Run(name, func(t *testing.T) {
			for _, origin := range []string{"", "http://other.example.com", "https://example.com", "http://example.com"} {
				request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/admin/private", nil)
				request.Header.Set("Origin", origin)
				request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token})
				called := false
				next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })
				response := httptest.NewRecorder()
				wrap(next).ServeHTTP(response, request)
				expected := http.StatusForbidden
				if origin == "http://example.com" {
					expected = http.StatusNoContent
				}
				if response.Code != expected || called != (expected == http.StatusNoContent) {
					t.Fatalf("origin %q: want %d, got %d (called=%v)", origin, expected, response.Code, called)
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("owner response is cacheable")
				}
			}
		})
	}
}

func TestOwnerMutationPassesThroughAuditBoundary(t *testing.T) {
	router := setupSessionRouter(t)
	owner := boundaryOwner(t, router)
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/admin/private", nil)
	request.Pattern = "POST /api/v1/admin/private"
	request.Header.Set("Origin", "http://example.com")
	request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token})
	response := httptest.NewRecorder()
	router.recentAuthOwnerAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	page, err := router.security.ListAuditEvents(t.Context(), &owner.Principal, model.AuditFilter{Action: "admin.mutation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].ResourceID != request.Pattern {
		t.Fatal("owner mutation did not produce its audit event")
	}
}
