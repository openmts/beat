package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// recordingBackups 记录聚合器实际发出的查询，用于确认只读扫描的过滤条件与上限。
type recordingBackups struct {
	query store.BackupQuery
	page  model.BackupPage
	err   error
}

func (monitor *recordingBackups) ListBackupsPage(_ context.Context, query store.BackupQuery) (model.BackupPage, error) {
	monitor.query = query
	return monitor.page, monitor.err
}

func (monitor *recordingBackups) CountQuarantine(context.Context) (int, error) { return 0, monitor.err }

func newMigrationOverviewFixture(t *testing.T) (migrationOverviewSource, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	stores, err := openServerStores(t.Context(), filepath.Join(directory, "server.db"), filepath.Join(directory, "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stores.closeIgnoringErrors)
	return migrationOverviewSource{sqlite: stores.sqlite, mts: stores.mts, backups: stores.backups, dataDir: directory, version: "test-version"}, directory
}

func TestNewMigrationOverviewSourceRequiresSQLiteStore(t *testing.T) {
	if source := newMigrationOverviewSource(nil, t.TempDir()); source != nil {
		t.Fatalf("nil stores produced %T", source)
	}
	if source := newMigrationOverviewSource(&serverStores{}, t.TempDir()); source != nil {
		t.Fatalf("stores without sqlite produced %T", source)
	}
	directory := t.TempDir()
	stores, err := openServerStores(t.Context(), filepath.Join(directory, "server.db"), filepath.Join(directory, "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stores.closeIgnoringErrors)
	source, ok := newMigrationOverviewSource(stores, directory).(migrationOverviewSource)
	if !ok {
		t.Fatal("source has unexpected type")
	}
	if source.version != buildinfo.Version || source.dataDir != directory || source.sqlite != stores.sqlite {
		t.Fatalf("source: %+v", source)
	}
}

func TestMigrationOverviewReportsCurrentSchema(t *testing.T) {
	source, _ := newMigrationOverviewFixture(t)
	view, err := source.MigrationOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != handler.MigrationStateCurrent || view.Code != handler.MigrationCodeOK {
		t.Fatalf("state: %s/%s", view.State, view.Code)
	}
	if view.ApplicationHealth != "ok" || view.MetricsHealth != "ok" {
		t.Fatalf("health: %s/%s", view.ApplicationHealth, view.MetricsHealth)
	}
	if view.CurrentSchema != store.MaximumSupportedSchemaVersion ||
		view.MinimumSupportedSchema != store.MinimumSupportedSchemaVersion ||
		view.MaximumSupportedSchema != store.MaximumSupportedSchemaVersion {
		t.Fatalf("schema bounds: %+v", view)
	}
	if view.LatestCompleted == nil || view.LatestCompleted.Version != store.MaximumSupportedSchemaVersion ||
		view.LatestCompleted.Name != store.MigrationName(store.MaximumSupportedSchemaVersion) {
		t.Fatalf("latest completed: %+v", view.LatestCompleted)
	}
	if len(view.PendingMigrations) != 0 || view.LastFailure != nil || view.RestorePending || view.RestoreStaged {
		t.Fatalf("unexpected extras: %+v", view)
	}
	if view.RunbookHash != migrationRunbookHash || view.Version != "test-version" {
		t.Fatalf("metadata: %+v", view)
	}
}

func TestMigrationOverviewReportsFailureMarker(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected string
	}{
		{name: "keeps code", code: store.MigrationFailureApply, expected: store.MigrationFailureApply},
		{name: "falls back", code: "", expected: handler.MigrationCodeUnreadable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source, directory := newMigrationOverviewFixture(t)
			writer := migrationstatus.NewWriter(directory, "0.2.0-dev", "test-build")
			if err := writer.Failed(test.code, true); err != nil {
				t.Fatal(err)
			}
			view, err := source.MigrationOverview(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if view.State != handler.MigrationStateInconsistent || view.Code != test.expected {
				t.Fatalf("state: %s/%s", view.State, view.Code)
			}
			if view.LastFailure == nil || view.LastFailure.State != migrationstatus.StateFailed ||
				view.LastFailure.FailureCode != test.expected || !view.LastFailure.RollbackRequired {
				t.Fatalf("failure: %+v", view.LastFailure)
			}
		})
	}
}

func TestMigrationOverviewReportsPendingRestore(t *testing.T) {
	source, directory := newMigrationOverviewFixture(t)
	journal := model.RestoreJournal{BackupID: "backup-1", ArchivePath: "/tmp/archive.tar", State: "pending",
		CreatedAt: time.Now().UTC()}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "restore.pending.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := source.MigrationOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !view.RestorePending || view.State != handler.MigrationStatePendingRestore || view.Code != handler.MigrationCodePendingRestore {
		t.Fatalf("view: %+v", view)
	}
}

func TestMigrationOverviewReportsUnknownRestoreState(t *testing.T) {
	source, directory := newMigrationOverviewFixture(t)
	if err := os.WriteFile(filepath.Join(directory, "restore.pending.json"), []byte(`{"backup_id":"broken"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := source.MigrationOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != handler.MigrationStateInconsistent || view.Code != handler.MigrationCodeRestoreUnknown || view.RestorePending {
		t.Fatalf("view: %+v", view)
	}
}

func TestMigrationOverviewScansStagedRestores(t *testing.T) {
	cases := []struct {
		name       string
		monitor    backupMonitor
		expected   bool
		expectScan bool
	}{
		{name: "absent monitor", monitor: nil},
		{name: "empty page", monitor: &recordingBackups{}, expectScan: true},
		{name: "staged record", monitor: &recordingBackups{page: model.BackupPage{Items: []model.BackupRecord{{ID: "b"}}}}, expected: true, expectScan: true},
		{name: "scan failure", monitor: &recordingBackups{err: backup.ErrNotFound}, expectScan: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source, _ := newMigrationOverviewFixture(t)
			source.backups = test.monitor
			view, err := source.MigrationOverview(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if view.RestoreStaged != test.expected {
				t.Fatalf("staged: %t", view.RestoreStaged)
			}
			if recorder, ok := test.monitor.(*recordingBackups); ok && test.expectScan {
				if recorder.query.State != model.BackupStateStaged || recorder.query.Limit != migrationOverviewStagedScanCap {
					t.Fatalf("query: %+v", recorder.query)
				}
			}
		})
	}
}
