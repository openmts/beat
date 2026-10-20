package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
)

type applicationSecretProbe struct{ calls int }

func (*applicationSecretProbe) Status() applicationsecrets.Status {
	return applicationsecrets.Status{Healthy: true}
}

func (probe *applicationSecretProbe) StartOwnerRotation(context.Context, applicationsecrets.RotationActor) (applicationsecrets.Status, error) {
	probe.calls++
	return probe.Status(), nil
}

func TestApplicationSecretRotationRequiresOwnerRecentAuthAndOrigin(t *testing.T) {
	probe := new(applicationSecretProbe)
	router := setupSessionRouterWithOptions(t, WithApplicationSecrets(probe))
	owner, err := router.security.Bootstrap(t.Context(), adminauth.BootstrapRequest{BootstrapToken: "admin-secret", Username: "owner", DisplayName: "Owner", Password: "owner password long enough"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		token, origin string
		status        int
	}{{"", "", http.StatusUnauthorized}, {owner.Token, "http://other.example", http.StatusForbidden}, {owner.Token, "http://example.com", http.StatusPreconditionRequired}} {
		response := secretRouteRequest(router, request.token, request.origin)
		if response.Code != request.status || probe.calls != 0 {
			t.Fatalf("rotation auth boundary: %d %s", response.Code, response.Body.String())
		}
	}
	if err := router.security.Reauthenticate(t.Context(), &owner.Principal, "owner password long enough", ""); err != nil {
		t.Fatal(err)
	}
	response := secretRouteRequest(router, owner.Token, "http://example.com")
	if response.Code != http.StatusAccepted || probe.calls != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authorized rotation: %d %s", response.Code, response.Body.String())
	}
	page, err := router.security.ListAuditEvents(t.Context(), &owner.Principal, model.AuditFilter{Action: "admin.mutation"})
	if err != nil || len(page.Events) == 0 {
		t.Fatalf("rotation audit missing: %v", err)
	}
}

func secretRouteRequest(router *Router, token, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/admin/application-secrets/rotate", nil)
	request.Header.Set("Origin", origin)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: token})
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}

func TestApplicationSecretRoutesRequireConfiguredSecurity(t *testing.T) {
	router := setupSessionRouter(t)
	if got := requestRouter(router, http.MethodGet, "/api/v1/admin/application-secrets", "", "", "").Code; got != http.StatusNotFound {
		t.Fatalf("unconfigured secret route = %d", got)
	}
}
