package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetricDashboardHTTPPersistenceAndConflict(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("personal").Definition
	create := fixture.request(t, http.MethodPost, dashboardTestRequest{body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleCreate})
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body)
	}
	created := dashboardResponse(t, create)
	definition.Title = "My capacity"
	update := fixture.request(t, http.MethodPut, dashboardTestRequest{id: created.ID, body: dashboardRequest{Revision: 1, Definition: definition}, handle: fixture.handler.HandleUpdate})
	if update.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", update.Code, update.Body)
	}
	conflict := fixture.request(t, http.MethodPut, dashboardTestRequest{id: created.ID, body: dashboardRequest{Revision: 1, Definition: definition}, handle: fixture.handler.HandleUpdate})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale browser saved: %d", conflict.Code)
	}
	rollback := fixture.request(t, http.MethodPost, dashboardTestRequest{id: created.ID, body: map[string]int{"revision": 2}, handle: fixture.handler.HandleRollback})
	if rollback.Code != http.StatusOK || dashboardResponse(t, rollback).Definition.Title != created.Definition.Title {
		t.Fatalf("rollback = %d: %s", rollback.Code, rollback.Body)
	}
	list := fixture.request(t, http.MethodGet, dashboardTestRequest{handle: fixture.handler.HandleList})
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.ID) || !strings.Contains(list.Body.String(), model.AdminDashboardID) {
		t.Fatal("saved and default dashboards not listed")
	}
	deleted := fixture.request(t, http.MethodDelete, dashboardTestRequest{id: created.ID, query: "?revision=3", handle: fixture.handler.HandleDelete})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body)
	}
}

func TestPublicMetricDashboardHidesCurrentPrivatePolicyAndActors(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("public").Definition
	update := fixture.request(t, http.MethodPut, dashboardTestRequest{id: model.PublicDashboardID, body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleUpdate})
	if update.Code != http.StatusOK {
		t.Fatalf("publish = %d: %s", update.Code, update.Body)
	}
	if _, err := fixture.db.DB.Exec("UPDATE metric_definitions SET public=0 WHERE metric_key='cpu'"); err != nil {
		t.Fatal(err)
	}
	public := fixture.request(t, http.MethodGet, dashboardTestRequest{handle: fixture.handler.HandleDefault})
	if public.Code != http.StatusOK {
		t.Fatalf("public = %d: %s", public.Code, public.Body)
	}
	for _, forbidden := range []string{`"metric":"cpu"`, "private-admin-id", "private-prefix", "updated_by"} {
		if strings.Contains(public.Body.String(), forbidden) {
			t.Fatalf("private metadata leaked: %s", forbidden)
		}
	}
	rejected := fixture.request(t, http.MethodPut, dashboardTestRequest{id: model.PublicDashboardID, body: dashboardRequest{Revision: 1, Definition: definition}, handle: fixture.handler.HandleUpdate})
	if rejected.Code != http.StatusBadRequest {
		t.Fatal("private metric was published")
	}
}

func TestMetricDashboardNodeTemplateAndHiddenNode(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("node").Definition
	definition.Title = "Node-specific layout"
	id := "node-" + fixture.node.ID
	response := fixture.request(t, http.MethodPut, dashboardTestRequest{id: id, body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleUpdate})
	if response.Code != http.StatusOK {
		t.Fatalf("node publication = %d: %s", response.Code, response.Body)
	}
	response = fixture.request(t, http.MethodGet, dashboardTestRequest{query: "?node_id=" + fixture.node.ID, handle: fixture.handler.HandleDefault})
	if response.Code != http.StatusOK || dashboardResponse(t, response).ID != id {
		t.Fatal("node template not selected")
	}
	if _, err := fixture.db.DB.Exec("UPDATE nodes SET is_public=0 WHERE id=?", fixture.node.ID); err != nil {
		t.Fatal(err)
	}
	response = fixture.request(t, http.MethodGet, dashboardTestRequest{query: "?node_id=" + fixture.node.ID, handle: fixture.handler.HandleDefault})
	if response.Code != http.StatusNotFound {
		t.Fatal("hidden node dashboard exposed")
	}
}

func TestMetricDashboardPublicationNeedsRecentOwner(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("public").Definition
	fixture.principal.Session.ReauthenticatedUntil = nil
	response := fixture.request(t, http.MethodPut, dashboardTestRequest{id: model.PublicDashboardID, body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleUpdate})
	if response.Code != http.StatusPreconditionRequired {
		t.Fatal("publication accepted without recent proof")
	}
	fixture.principal.User.Role = model.AdminRoleAdmin
	response = fixture.request(t, http.MethodPut, dashboardTestRequest{id: model.PublicDashboardID, body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleUpdate})
	if response.Code != http.StatusForbidden {
		t.Fatal("admin published global layout")
	}
}
