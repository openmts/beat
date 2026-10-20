package main

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/store"
)

func TestMigrationOverviewReportsSchemaBehindLedger(t *testing.T) {
	source, _ := newMigrationOverviewFixture(t)
	if _, err := source.sqlite.DB.ExecContext(t.Context(),
		"DELETE FROM schema_migrations WHERE version = ?", store.MaximumSupportedSchemaVersion); err != nil {
		t.Fatal(err)
	}
	view, err := source.MigrationOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != handler.MigrationStateUpgradePending || view.Code != handler.MigrationCodeSchemaMismatch {
		t.Fatalf("state: %s/%s", view.State, view.Code)
	}
	if view.ApplicationHealth != "degraded" || view.CurrentSchema != store.MaximumSupportedSchemaVersion-1 {
		t.Fatalf("view: %+v", view)
	}
	if view.LatestCompleted == nil || view.LatestCompleted.Version != store.MaximumSupportedSchemaVersion-1 {
		t.Fatalf("latest completed: %+v", view.LatestCompleted)
	}
	if len(view.PendingMigrations) != 1 || view.PendingMigrations[0].Version != store.MaximumSupportedSchemaVersion {
		t.Fatalf("pending: %+v", view.PendingMigrations)
	}
}

func TestMigrationOverviewReportsUnreadableSchema(t *testing.T) {
	directory := t.TempDir()
	stores, err := openServerStores(t.Context(), filepath.Join(directory, "server.db"), filepath.Join(directory, "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stores.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stores.closeIgnoringErrors() })
	source := migrationOverviewSource{sqlite: stores.sqlite, dataDir: directory, version: "test-version"}
	view, err := source.MigrationOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != handler.MigrationStateInconsistent || view.Code != handler.MigrationCodeUnreadable {
		t.Fatalf("state: %s/%s", view.State, view.Code)
	}
	if view.ApplicationHealth != "degraded" || view.LatestCompleted != nil {
		t.Fatalf("view: %+v", view)
	}
}

func TestMigrationOverviewReportsMetricsHealth(t *testing.T) {
	cases := []struct {
		name     string
		monitor  mtsMonitor
		expected string
	}{
		{name: "absent monitor", monitor: nil, expected: "ok"},
		{name: "healthy", monitor: fakeMTSMonitor{healthy: true}, expected: "ok"},
		{name: "unhealthy", monitor: fakeMTSMonitor{}, expected: "degraded"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source, _ := newMigrationOverviewFixture(t)
			source.mts = test.monitor
			view, err := source.MigrationOverview(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if view.MetricsHealth != test.expected {
				t.Fatalf("metrics health: %s", view.MetricsHealth)
			}
			if view.ApplicationHealth != "ok" {
				t.Fatalf("application health: %s", view.ApplicationHealth)
			}
		})
	}
}
