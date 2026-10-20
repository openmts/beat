package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipalhttp"
)

func TestMetadataAdminRejectsNonAdministrativeRole(t *testing.T) {
	router := &Router{}
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/nodes", nil)
	principal := model.AdminPrincipal{User: model.AdminUser{Role: model.AdminRole("viewer")}}
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	recorder := httptest.NewRecorder()
	router.metadataAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("non-administrative role must not reach the handler")
	}), false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("metadata admin status = %d", recorder.Code)
	}
}

func TestMetadataActorRequiresAdministrativePrincipal(t *testing.T) {
	router := &Router{}
	recorder := httptest.NewRecorder()
	router.metadataActor(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request must not reach the handler")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("metadata actor status = %d", recorder.Code)
	}
}

func TestGuardedAdminOperationWithoutSessionService(t *testing.T) {
	router := &Router{}
	recorder := httptest.NewRecorder()
	router.guardedAdminOperation(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("guarded operation status = %d", recorder.Code)
	}
}

func TestServiceTokenPrivateRoutesFallBackToAdminGuard(t *testing.T) {
	router := &Router{}
	if guard := router.serviceTokenPrivateRoute(model.ServiceScopeNodesRead, nil); guard == nil {
		t.Fatal("private route guard is nil")
	}
	if guard := router.serviceTokenPrivateResourceRoute(
		model.ServiceScopeNodesRead, model.ServiceResourceNode, nil, nil,
	); guard == nil {
		t.Fatal("private resource guard is nil")
	}
}

func TestEffectiveAPIOriginsKeepsConfiguredPolicy(t *testing.T) {
	config, err := deploymentaccess.Load(func(key string) string {
		if key == deploymentaccess.EnvPublicBaseURL {
			return "https://beat.example.com"
		}
		return "https://console.example.com"
	})
	if err != nil {
		t.Fatalf("load deployment access: %v", err)
	}
	router := &Router{}
	WithDeploymentAccess(config)(router)
	origins := router.effectiveAPIOrigins()
	if origins.Mode() != deploymentaccess.OriginModeConfigured || origins.Count() != 1 {
		t.Fatalf("origins = %+v", origins)
	}
}

func TestRequireSameOriginRejectsForeignOrigin(t *testing.T) {
	router := setupSessionRouter(t)
	response := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", `{}`, "",
		"http://attacker.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin bootstrap status = %d", response.Code)
	}
}

func TestServicePrincipalGuardWithoutAdministratorSession(t *testing.T) {
	router := &Router{servicePrincipals: serviceprincipalhttp.New(nil)}
	mux := http.NewServeMux()
	router.registerServicePrincipalRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "http://example.com/api/v1/admin/service-tokens", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("service token guard status = %d", recorder.Code)
	}
}

func TestServicePrincipalGuardUsesSessionGuard(t *testing.T) {
	router := setupSessionRouter(t)
	recorder := httptest.NewRecorder()
	guard := router.serviceTokenGuard(false)
	guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "http://example.com/api/v1/admin/service-tokens", nil))
	if recorder.Code == http.StatusTeapot {
		t.Fatal("service token guard passed an unauthenticated request through")
	}
}
