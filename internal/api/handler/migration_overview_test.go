package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeMigrationOverview struct {
	view MigrationOverviewView
	err  error
}

func (fake fakeMigrationOverview) MigrationOverview(context.Context) (MigrationOverviewView, error) {
	return fake.view, fake.err
}

func TestMigrationOverviewHandlerReturnsBoundedView(t *testing.T) {
	source := fakeMigrationOverview{view: MigrationOverviewView{State: MigrationStateCurrent, Code: MigrationCodeOK,
		CurrentSchema: 25, MaximumSupportedSchema: 25, ApplicationHealth: "ok", MetricsHealth: "ok",
		PendingMigrations: []MigrationStepView{}}}
	recorder := httptest.NewRecorder()
	NewMigrationOverviewHandler(source).HandleOverview(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/migration", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"current_schema":25`) {
		t.Fatalf("overview: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMigrationOverviewHandlerFailsClosed(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewMigrationOverviewHandler(nil).HandleOverview(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/migration", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil source: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	NewMigrationOverviewHandler(fakeMigrationOverview{err: errors.New("private-detail")}).HandleOverview(
		recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/migration", nil))
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "private-detail") {
		t.Fatalf("source failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMigrationStepsAreBounded(t *testing.T) {
	if len(MigrationSteps(nil)) != 0 {
		t.Fatal("nil steps must stay empty")
	}
	pending := make([]MigrationStepView, migrationPendingLimit+5)
	if len(MigrationSteps(pending)) != migrationPendingLimit {
		t.Fatal("pending steps are unbounded")
	}
}
