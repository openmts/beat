package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDashboardPersistsRevisionMetadata(t *testing.T) {
	fixture := newDashboardFixture(t)
	first, second := fixture.twoRevisions(t)
	if first.Revision != 1 || first.HasPrevious {
		t.Fatal("initial revision is incorrect")
	}
	if second.Revision != 2 || !second.HasPrevious || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("revision metadata is incorrect")
	}
	stored, err := fixture.dashboards.Get(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Definition.Title != "Production capacity" {
		t.Fatal("saved layout missing")
	}
}

func TestDashboardRollbackPreservesPreviousDefinition(t *testing.T) {
	fixture := newDashboardFixture(t)
	first, second := fixture.twoRevisions(t)
	previous, err := fixture.dashboards.Previous(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Title != first.Definition.Title {
		t.Fatal("previous layout missing")
	}
	restored, err := fixture.dashboards.Rollback(t.Context(), DashboardDelete{ID: second.ID, ExpectedRevision: 2, Actor: fixture.actor})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Revision != 3 || restored.Definition.Title != first.Definition.Title {
		t.Fatal("rollback changed the wrong definition")
	}
	page, err := fixture.admins.ListAuditEvents(t.Context(), model.AuditFilter{Action: "metrics.dashboard.rollback"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].ResourceID != second.ID {
		t.Fatal("rollback audit missing")
	}
}

func TestDashboardDeleteRemovesOnlySavedLayout(t *testing.T) {
	fixture := newDashboardFixture(t)
	_, second := fixture.twoRevisions(t)
	if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: second.ID, ExpectedRevision: 2, Actor: fixture.actor}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := fixture.dashboards.Get(t.Context(), second.ID); err != nil || deleted != nil {
		t.Fatal("deleted dashboard remains")
	}
}

func TestDashboardCompareAndSwapRejectsStaleEdits(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	first := dashboardSave(t, fixture, write)
	write.Value.Definition.Title = "stale browser tab"
	if _, err := fixture.dashboards.Save(t.Context(), write); !errors.Is(err, ErrDashboardConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: first.ID, ExpectedRevision: 0, Actor: fixture.actor}); !errors.Is(err, ErrDashboardConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	write.ExpectedRevision = 1
	dashboardSave(t, fixture, write)
	if _, err := fixture.dashboards.Rollback(t.Context(), DashboardDelete{ID: first.ID, ExpectedRevision: 1, Actor: fixture.actor}); !errors.Is(err, ErrDashboardConflict) {
		t.Fatalf("stale rollback: %v", err)
	}
	missing := fixture.personalWrite()
	missing.ExpectedRevision = 9
	if _, err := fixture.dashboards.Save(t.Context(), missing); !errors.Is(err, ErrDashboardConflict) {
		t.Fatal("missing prior revision accepted")
	}
}

func TestDashboardPersonalOwnershipAndListPrivacy(t *testing.T) {
	fixture := newDashboardFixture(t)
	first := dashboardSave(t, fixture, fixture.personalWrite())
	other := fixture.addActor(t, "operator", model.AdminRoleAdmin)
	write := DashboardWrite{Value: *first, ExpectedRevision: first.Revision, Actor: other}
	if _, err := fixture.dashboards.Save(t.Context(), write); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("another user overwrote layout: %v", err)
	}
	if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: first.ID, ExpectedRevision: 1, Actor: other}); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("another user deleted layout: %v", err)
	}
	values, err := fixture.dashboards.List(t.Context(), other.UserID)
	if err != nil || len(values) != 0 {
		t.Fatalf("personal layout leaked through list: %v", err)
	}
	values, err = fixture.dashboards.List(t.Context(), fixture.actor.UserID)
	if err != nil || len(values) != 1 {
		t.Fatalf("owner layout missing: %v", err)
	}
}

func TestDashboardSharedTemplatesRequireCurrentOwnerAndProof(t *testing.T) {
	fixture := newDashboardFixture(t)
	operator := fixture.addActor(t, "operator", model.AdminRoleAdmin)
	value := model.DefaultMetricDashboard("public")
	if _, err := fixture.dashboards.Save(t.Context(), DashboardWrite{Value: value, Actor: operator}); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatal("administrator published a public layout")
	}
	if err := fixture.admins.MarkSessionReauthenticated(t.Context(), fixture.actor.SessionID, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.Save(t.Context(), DashboardWrite{Value: value, Actor: fixture.actor}); !errors.Is(err, ErrDashboardForbidden) {
		t.Fatal("expired recent proof published a layout")
	}
	value.Scope, value.OwnerUserID = "personal", fixture.actor.UserID
	if _, err := fixture.dashboards.Save(t.Context(), DashboardWrite{Value: value, Actor: fixture.actor}); !errors.Is(err, model.ErrInvalidDashboard) {
		t.Fatal("personal layout occupied reserved default ID")
	}
}

func TestDashboardMissingAndPersonalLimit(t *testing.T) {
	fixture := newDashboardFixture(t)
	for range model.MaxPersonalDashboards {
		dashboardSave(t, fixture, fixture.personalWrite())
	}
	if _, err := fixture.dashboards.Save(t.Context(), fixture.personalWrite()); !errors.Is(err, ErrDashboardLimit) {
		t.Fatal("personal limit bypassed")
	}
	if _, err := fixture.dashboards.Previous(t.Context(), "missing"); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatal("missing previous layout accepted")
	}
	if _, err := fixture.dashboards.Rollback(t.Context(), DashboardDelete{ID: "missing", Actor: fixture.actor}); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatal("missing rollback accepted")
	}
	if err := fixture.dashboards.Delete(t.Context(), DashboardDelete{ID: "missing", Actor: fixture.actor}); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatal("missing delete accepted")
	}
}
