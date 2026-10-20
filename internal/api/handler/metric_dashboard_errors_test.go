package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestMetricDashboardAuthenticationAndAvailabilityErrors(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	handles := []http.HandlerFunc{fixture.handler.HandleList, fixture.handler.HandleCreate,
		fixture.handler.HandleUpdate, fixture.handler.HandleDelete, fixture.handler.HandleRollback}
	for _, handle := range handles {
		response := httptest.NewRecorder()
		handle(response, httptest.NewRequest(http.MethodPost, "/", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous mutation = %d", response.Code)
		}
	}
	fixture.handler.dashboards = nil
	for _, handle := range handles {
		response := fixture.request(t, http.MethodPost, dashboardTestRequest{handle: handle})
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unconfigured mutation = %d", response.Code)
		}
	}
}

func TestMetricDashboardRejectsUnknownTrailingAndOversizedBodies(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	for _, body := range []string{"{", `{"unexpected":1}`, `{ } { }`, strings.Repeat(" ", model.MaxDashboardBytes+1024) + `{}`} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), fixture.principal))
		response := httptest.NewRecorder()
		fixture.handler.HandleCreate(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid body = %d", response.Code)
		}
	}
	for _, handle := range []http.HandlerFunc{fixture.handler.HandleUpdate, fixture.handler.HandleRollback} {
		response := fixture.request(t, http.MethodPost, dashboardTestRequest{id: model.PublicDashboardID, body: map[string]int{"unknown": 1}, handle: handle})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid update body = %d", response.Code)
		}
	}
}

func TestMetricDashboardMissingTargetsAndInvalidDeleteRevision(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	for _, handle := range []http.HandlerFunc{fixture.handler.HandleUpdate, fixture.handler.HandleDelete, fixture.handler.HandleRollback} {
		response := fixture.request(t, http.MethodPut, dashboardTestRequest{id: "missing", query: "?revision=1", body: map[string]int{"revision": 1}, handle: handle})
		if response.Code != http.StatusNotFound {
			t.Fatalf("missing target = %d", response.Code)
		}
	}
	response := fixture.request(t, http.MethodDelete, dashboardTestRequest{id: model.PublicDashboardID, handle: fixture.handler.HandleDelete})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing delete revision = %d", response.Code)
	}
	response = fixture.request(t, http.MethodPost, dashboardTestRequest{id: model.PublicDashboardID, body: map[string]int{"revision": 0}, handle: fixture.handler.HandleRollback})
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing rollback history = %d", response.Code)
	}
}

func TestMetricDashboardPolicyFailsClosed(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	fixture.handler.policy = nil
	response := fixture.request(t, http.MethodGet, dashboardTestRequest{handle: fixture.handler.HandleDefault})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("default rendered without configured policy")
	}
	response = fixture.request(t, http.MethodPost, dashboardTestRequest{body: dashboardRequest{Definition: model.DefaultMetricDashboard("public").Definition}, handle: fixture.handler.HandleCreate})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("write accepted without policy")
	}
	fixture.handler.policy = func(context.Context, bool) (model.DashboardPolicy, error) {
		return model.DashboardPolicy{}, errors.New("storage failed")
	}
	response = fixture.request(t, http.MethodGet, dashboardTestRequest{handle: fixture.handler.HandleDefault})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("failed catalog became a public template")
	}
	if _, err := NewDashboardPolicyReader(nil, nil)(t.Context(), true); err == nil {
		t.Fatal("nil policy dependencies accepted")
	}
}

func TestMetricDashboardClosedStoreFailures(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, handle := range []http.HandlerFunc{fixture.handler.HandleDefault, fixture.handler.HandleList, fixture.handler.HandleUpdate, fixture.handler.HandleRollback} {
		response := fixture.request(t, http.MethodGet, dashboardTestRequest{id: model.PublicDashboardID, handle: handle})
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("closed storage = %d", response.Code)
		}
	}
	if _, err := fixture.handler.nodeDashboard(t.Context(), fixture.node.ID); err == nil {
		t.Fatal("closed node lookup accepted")
	}
	if _, err := fixture.handler.policy(t.Context(), true); err == nil {
		t.Fatal("closed catalog accepted")
	}
}

func TestMetricDashboardErrorClassification(t *testing.T) {
	for failure, status := range map[error]int{store.ErrDashboardLimit: http.StatusConflict, store.ErrDashboardForbidden: http.StatusForbidden,
		store.ErrDashboardConflict: http.StatusConflict, model.ErrInvalidDashboard: http.StatusBadRequest, errDashboardRecentAuth: http.StatusPreconditionRequired} {
		response := httptest.NewRecorder()
		respondDashboardError(response, failure)
		if response.Code != status {
			t.Fatalf("error classification = %d", response.Code)
		}
	}
}
