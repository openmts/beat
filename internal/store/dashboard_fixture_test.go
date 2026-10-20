package store

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type dashboardFixture struct {
	database   *SQLiteStore
	dashboards *DashboardStore
	admins     *AdminStore
	actor      DashboardActor
}

func newDashboardFixture(t *testing.T) dashboardFixture {
	t.Helper()
	database := setupTestDB(t)
	fixture := dashboardFixture{database: database, dashboards: NewDashboardStore(database.DB), admins: NewAdminStore(database.DB)}
	fixture.actor = fixture.addActor(t, "owner", model.AdminRoleOwner)
	return fixture
}

func (fixture dashboardFixture) addActor(t *testing.T, name string, role model.AdminRole) DashboardActor {
	t.Helper()
	user := testAdminUser(name, role)
	if err := fixture.admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	now, until := model.NowUTC(), model.NowUTC().Add(time.Hour)
	session := model.AdminSession{ID: uuid.NewString(), UserID: user.ID, TokenHash: []byte(uuid.NewString()),
		TokenPrefix: "display-prefix", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until,
		AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}
	if err := fixture.admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return DashboardActor{UserID: user.ID, SessionID: session.ID, RequestID: "request-id", IPAddress: "127.0.0.1", UserAgent: "test"}
}

func (fixture dashboardFixture) personalWrite() DashboardWrite {
	value := model.DefaultMetricDashboard("personal")
	value.ID, value.OwnerUserID = uuid.NewString(), fixture.actor.UserID
	return DashboardWrite{Value: value, Actor: fixture.actor}
}

func dashboardSave(t *testing.T, fixture dashboardFixture, write DashboardWrite) *model.MetricDashboard {
	t.Helper()
	value, err := fixture.dashboards.Save(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (fixture dashboardFixture) twoRevisions(t *testing.T) (*model.MetricDashboard, *model.MetricDashboard) {
	t.Helper()
	write := fixture.personalWrite()
	first := dashboardSave(t, fixture, write)
	write.ExpectedRevision, write.Value.Definition.Title = 1, "Production capacity"
	return first, dashboardSave(t, fixture, write)
}
