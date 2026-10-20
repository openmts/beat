package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

func TestRuntimeRouteRequiresCurrentOwnerWithoutRecentProof(t *testing.T) {
	called := 0
	router := setupSessionRouterWithOptions(t, WithRuntimeStatus(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		handler.JSONResponse(w, 200, map[string]bool{"stopping": false})
	})))
	owner := boundaryOwner(t, router)
	fresh, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: "owner", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Principal.Session.RecentlyAuthenticated(router.security.Now()) {
		t.Fatal("fixture unexpectedly has recent proof")
	}
	cookie := &http.Cookie{Name: handler.AdminSessionCookieName, Value: fresh.Token}
	response := requestRouterWithCookie(router, "GET", "/api/v1/admin/runtime", "", cookie, "")
	if response.Code != 200 || called != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Body.String())
	}
	user, err := router.security.CreateUser(t.Context(), &owner.Principal, adminauth.CreateUserRequest{Username: "operator", DisplayName: "Operator", Role: model.AdminRoleAdmin, Password: "operator password long"})
	if err != nil {
		t.Fatal(err)
	}
	_ = user
	login, err := router.security.Login(t.Context(), adminauth.LoginRequest{Username: "operator", Password: "operator password long"})
	if err != nil {
		t.Fatal(err)
	}
	response = requestRouterWithCookie(router, "GET", "/api/v1/admin/runtime", "", &http.Cookie{Name: handler.AdminSessionCookieName, Value: login.Token}, "")
	if response.Code != 403 || called != 1 {
		t.Fatal(response.Code, response.Body.String())
	}
	anonymous := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(anonymous, httptest.NewRequest("GET", "/api/v1/admin/runtime", nil))
	if anonymous.Code != 401 {
		t.Fatal(anonymous.Code)
	}
}

func TestMetricsStreamRouteOnlyUsesInstalledSharedHandler(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()
	if value, err := router.BuildMetricsSnapshot(t.Context()); err == nil || value.Value != nil {
		t.Fatal("missing metrics store looked healthy", value, err)
	}
	called := 0
	router.SetMetricsStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(503) }))
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/ws/metrics", nil))
	if response.Code != 503 || called != 1 {
		t.Fatal(response.Code, called)
	}
}
