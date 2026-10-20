package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDashboardAuditFailureRollsBackMutation(t *testing.T) {
	for _, failure := range []string{"ABORT", "IGNORE"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newDashboardFixture(t)
			write := fixture.personalWrite()
			body := "SELECT RAISE(IGNORE);"
			if failure == "ABORT" {
				body = "SELECT RAISE(ABORT,'audit unavailable');"
			}
			if _, err := fixture.database.DB.Exec("CREATE TRIGGER fail_dashboard_audit BEFORE INSERT ON admin_audit_events BEGIN " + body + " END"); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.dashboards.Save(t.Context(), write); err == nil {
				t.Fatal("audit failure reported a successful save")
			}
			stored, err := fixture.dashboards.Get(t.Context(), write.Value.ID)
			if err != nil || stored != nil {
				t.Fatalf("unaudited layout persisted: %v", err)
			}
		})
	}
}

func TestDashboardSessionRevocationPreventsMutation(t *testing.T) {
	fixture := newDashboardFixture(t)
	if err := fixture.admins.RevokeSession(t.Context(), fixture.actor.SessionID, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Save(t.Context(), fixture.personalWrite()); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("revoked session saved layout: %v", err)
	}
}

func TestDashboardStoreRejectsCorruptData(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	value := dashboardSave(t, fixture, write)
	if _, err := fixture.database.DB.Exec("UPDATE metric_dashboards SET definition_json='{}' WHERE id=?", value.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Get(t.Context(), value.ID); err == nil {
		t.Fatal("corrupt definition accepted")
	}
	if _, err := fixture.dashboards.List(t.Context(), fixture.actor.UserID); err == nil {
		t.Fatal("corrupt list accepted")
	}
}

func TestDashboardStoreRejectsClosedOperations(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	value := dashboardSave(t, fixture, write)
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Get(t.Context(), value.ID); err == nil {
		t.Fatal("closed read accepted")
	}
	if _, err := fixture.dashboards.List(t.Context(), fixture.actor.UserID); err == nil {
		t.Fatal("closed list accepted")
	}
	if _, err := fixture.dashboards.Previous(t.Context(), value.ID); err == nil {
		t.Fatal("closed previous read accepted")
	}
	if _, err := fixture.dashboards.Save(t.Context(), write); err == nil {
		t.Fatal("closed save accepted")
	}
	if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: value.ID, Actor: fixture.actor}); err == nil {
		t.Fatal("closed delete accepted")
	}
	if _, err := fixture.dashboards.Rollback(t.Context(), DashboardDelete{ID: value.ID, Actor: fixture.actor}); err == nil {
		t.Fatal("closed rollback accepted")
	}
}

func TestDashboardEncodingAndTargetBoundaries(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	write.Value.Definition.Title = ""
	if _, err := fixture.dashboards.Save(t.Context(), write); !errors.Is(err, model.ErrInvalidDashboard) {
		t.Fatal("invalid layout saved")
	}
	for _, scope := range []string{"public", "admin", "node", "invalid"} {
		value := model.DefaultMetricDashboard(scope)
		value.ID = "invalid-target"
		if _, err := fixture.dashboards.Save(t.Context(), DashboardWrite{Value: value, Actor: fixture.actor}); !errors.Is(err, ErrDashboardForbidden) {
			t.Fatalf("invalid target: %v", err)
		}
	}
	if value := dashboardActorField(strings.Repeat("x", 100), 8); value != "xxxxxxxx" {
		t.Fatal("audit field budget ignored")
	}
	if _, err := decodePreviousDashboard("invalid-json"); err == nil {
		t.Fatal("corrupt prior JSON accepted")
	}
}
