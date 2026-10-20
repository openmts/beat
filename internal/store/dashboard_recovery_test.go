package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDashboardSharedTargetsAndIdentityCannotBeChanged(t *testing.T) {
	fixture := newDashboardFixture(t)
	for _, scope := range []string{"public", "admin"} {
		value := model.DefaultMetricDashboard(scope)
		saved := dashboardSave(t, fixture, DashboardWrite{Value: value, Actor: fixture.actor})
		if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: saved.ID, ExpectedRevision: 1, Actor: fixture.actor}); !errors.Is(err, ErrDashboardForbidden) {
			t.Fatal("shared template deleted")
		}
	}
	node, err := NewNodeStore(fixture.database.DB).UpsertNode(t.Context(), "node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	value := model.DefaultMetricDashboard("node")
	value.NodeID, value.ID = node.ID, "node-"+node.ID
	dashboardSave(t, fixture, DashboardWrite{Value: value, Actor: fixture.actor})
	write := fixture.personalWrite()
	saved := dashboardSave(t, fixture, write)
	other := fixture.addActor(t, "other", model.AdminRoleAdmin)
	write.Actor, write.ExpectedRevision, write.Value.OwnerUserID = other, 1, other.UserID
	if _, err := fixture.dashboards.Save(t.Context(), write); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("ownership changed: %v", err)
	}
	if _, err := fixture.dashboards.Previous(t.Context(), saved.ID); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatal("first revision has a previous layout")
	}
	write = fixture.personalWrite()
	write.ExpectedRevision = -1
	if _, err := fixture.dashboards.Save(t.Context(), write); !errors.Is(err, ErrDashboardConflict) {
		t.Fatal("negative revision accepted")
	}
}

func TestDashboardEveryMutationRollsBackOnAuditFailure(t *testing.T) {
	for _, action := range []string{"save", "delete", "rollback"} {
		t.Run(action, func(t *testing.T) {
			fixture := newDashboardFixture(t)
			write := fixture.personalWrite()
			first := dashboardSave(t, fixture, write)
			write.ExpectedRevision, write.Value.Definition.Title = 1, "Second version"
			second := dashboardSave(t, fixture, write)
			if _, err := fixture.database.DB.Exec(`CREATE TRIGGER reject_dashboard_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'failed'); END`); err != nil {
				t.Fatal(err)
			}
			request := DashboardDelete{ID: first.ID, ExpectedRevision: 2, Actor: fixture.actor}
			var err error
			switch action {
			case "save":
				write.ExpectedRevision = 2
				_, err = fixture.dashboards.Save(t.Context(), write)
			case "delete":
				err = fixture.dashboards.Delete(t.Context(), request)
			case "rollback":
				_, err = fixture.dashboards.Rollback(t.Context(), request)
			}
			if err == nil {
				t.Fatal("unaudited mutation succeeded")
			}
			current, err := fixture.dashboards.Get(t.Context(), first.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertDashboardUnchanged(t, current, second)
		})
	}
}

func TestDashboardFailedCommitDoesNotPublishMetadata(t *testing.T) {
	fixture := newDashboardFixture(t)
	for _, statement := range []string{
		`CREATE TABLE dashboard_commit_fault(value TEXT REFERENCES groups(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER dashboard_fault AFTER INSERT ON metric_dashboards BEGIN INSERT INTO dashboard_commit_fault VALUES('missing'); END`,
	} {
		if _, err := fixture.database.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	write := fixture.personalWrite()
	if _, err := fixture.dashboards.Save(t.Context(), write); err == nil {
		t.Fatal("failed commit reported a saved dashboard")
	}
	value, err := fixture.dashboards.Get(t.Context(), write.Value.ID)
	if err != nil || value != nil {
		t.Fatalf("failed commit leaked layout: %v", err)
	}
}

func TestDashboardLargeJSONAndMalformedHistoryFailClosed(t *testing.T) {
	definition := model.DefaultMetricDashboard("public").Definition
	definition.Panels = nil
	for panelIndex := range model.MaxDashboardPanels {
		panel := model.DefaultMetricDashboard("public").Definition.Panels[0]
		panel.ID, panel.Title = fmt.Sprint(panelIndex), strings.Repeat("t", 120)
		panel.Series = nil
		for seriesIndex := range model.MaxDashboardSeries {
			panel.Series = append(panel.Series, model.DashboardSeries{RefID: fmt.Sprint(seriesIndex), NodeID: strings.Repeat("n", 64),
				Metric: strings.Repeat("m", 128), Aggregation: "avg", Label: strings.Repeat("l", 120), Color: "blue"})
		}
		definition.Panels = append(definition.Panels, panel)
	}
	if _, err := encodeDashboard(definition); !errors.Is(err, model.ErrInvalidDashboard) {
		t.Fatal("oversized dashboard encoded")
	}
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	dashboardSave(t, fixture, write)
	if _, err := fixture.database.DB.Exec(`UPDATE metric_dashboards SET previous_definition_json='"invalid"' WHERE id=?`, write.Value.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Previous(t.Context(), write.Value.ID); err == nil {
		t.Fatal("malformed previous layout decoded")
	}
	if _, err := fixture.dashboards.Rollback(t.Context(), DashboardDelete{ID: write.Value.ID, ExpectedRevision: 1, Actor: fixture.actor}); err == nil {
		t.Fatal("malformed previous layout activated")
	}
	if _, err := fixture.database.DB.Exec(`UPDATE metric_dashboards SET definition_json='"invalid"' WHERE id=?`, write.Value.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Get(t.Context(), write.Value.ID); err == nil {
		t.Fatal("malformed current layout decoded")
	}
}

func assertDashboardUnchanged(t *testing.T, current, expected *model.MetricDashboard) {
	t.Helper()
	if current == nil || current.Revision != expected.Revision || current.Definition.Title != expected.Definition.Title {
		t.Fatal("failed mutation changed saved data")
	}
}
