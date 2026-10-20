package operatorrecovery

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/store"
)

func runStatus(t *testing.T, dataDir string) MigrationReport {
	t.Helper()
	var output bytes.Buffer
	if err := Run([]string{"migration-status", "--data-dir", dataDir}, &output); err != nil {
		t.Fatal(err)
	}
	var report MigrationReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestMigrationStatusEmptyDirectory(t *testing.T) {
	report := runStatus(t, privateTemp(t))
	if report.State != "empty" || report.Code != migrationCodeDatabaseAbsent || report.DatabasePresent || report.LockConflict {
		t.Fatalf("report: %+v", report)
	}
	if report.MinimumSupportedSchema != store.MinimumSupportedSchemaVersion || report.MaximumSupportedSchema != store.MaximumSupportedSchemaVersion {
		t.Fatalf("supported range: %+v", report)
	}
	if len(report.PendingMigrations) != 0 {
		t.Fatalf("pending: %+v", report.PendingMigrations)
	}
}

func TestMigrationStatusCurrentAndPending(t *testing.T) {
	config := fixture(t)
	report := runStatus(t, config.DataDir)
	if report.State != "current" || report.Code != migrationCodeOK || !report.Integrity || !report.DatabasePresent {
		t.Fatalf("report: %+v", report)
	}
	if report.CurrentSchema != store.MaximumSupportedSchemaVersion {
		t.Fatalf("current schema: %+v", report)
	}
	if report.DatabaseMode != "0600" || !report.DatabaseOwnedByUser {
		t.Fatalf("database ownership: %+v", report)
	}
	database, err := openDatabase(config.DatabasePath, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM schema_migrations WHERE version=?", store.MaximumSupportedSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	report = runStatus(t, config.DataDir)
	if report.State != "upgrade_pending" || report.Code != migrationCodeOK {
		t.Fatalf("report: %+v", report)
	}
	if len(report.PendingMigrations) != 1 || report.PendingMigrations[0].Version != store.MaximumSupportedSchemaVersion {
		t.Fatalf("pending: %+v", report.PendingMigrations)
	}
	after, err := os.ReadFile(config.DatabasePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("migration-status changed the live database")
	}
}

func TestMigrationStatusRejectsFutureAndBrokenLedgers(t *testing.T) {
	for name, statement := range map[string]string{
		"future":   "INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'future_schema', CURRENT_TIMESTAMP)",
		"gap":      "DELETE FROM schema_migrations WHERE version=2",
		"renamed":  "UPDATE schema_migrations SET name='renamed' WHERE version=3",
		"dangling": "DELETE FROM schema_migrations",
	} {
		t.Run(name, func(t *testing.T) {
			config := fixture(t)
			database, err := openDatabase(config.DatabasePath, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			report := runStatus(t, config.DataDir)
			switch name {
			case "future":
				if report.State != "failed" || report.Code != migrationCodeFutureSchema || !report.FutureSchema {
					t.Fatalf("report: %+v", report)
				}
			case "dangling":
				if report.Code != migrationCodeLedgerInvalid || !report.MissingLedger {
					t.Fatalf("report: %+v", report)
				}
			default:
				if report.Code != migrationCodeLedgerInvalid || report.MissingLedger {
					t.Fatalf("report: %+v", report)
				}
			}
		})
	}
}

func TestMigrationStatusReportsLockConflictAndLastFailure(t *testing.T) {
	config := fixture(t)
	writer := migrationstatus.NewWriter(config.DataDir, "0.2.0-test", "testcommit")
	if err := writer.Started(store.MaximumSupportedSchemaVersion-1, store.MaximumSupportedSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := writer.Failed(store.MigrationFailureApply, false); err != nil {
		t.Fatal(err)
	}
	lease, err := applicationsecrets.AcquireLease(config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Close() }()
	report := runStatus(t, config.DataDir)
	if report.State != "locked" || report.Code != migrationCodeLockConflict || !report.LockConflict {
		t.Fatalf("report: %+v", report)
	}
	if report.LastFailure == nil || report.LastFailure.State != migrationstatus.StateFailed || report.LastFailure.FailureCode != store.MigrationFailureApply {
		t.Fatalf("last failure: %+v", report.LastFailure)
	}
}

func TestMigrationStatusRejectsNonPrivateInputs(t *testing.T) {
	config := fixture(t)
	if err := os.Chmod(config.DatabasePath, 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"migration-status", "--data-dir", config.DataDir}, &output); err == nil {
		t.Fatal("insecure database accepted")
	}
	if err := os.Chmod(config.DatabasePath, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.DatabasePath, 0o644); err != nil {
		t.Fatal(err)
	}
	report := collectMigrationStatus(t.Context(), resolved)
	if report.State != "failed" || report.Code != migrationCodeInsecureMode {
		t.Fatalf("report: %+v", report)
	}
}

func TestMigrationStatusBackupArchiveEvidence(t *testing.T) {
	config := fixture(t)
	backups := filepath.Join(config.DataDir, "backups")
	if err := os.Mkdir(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, "beat-backup.tar.zst"), []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := runStatus(t, config.DataDir)
	if !report.BackupArchivePresent || report.BackupArchiveCount != 1 {
		t.Fatalf("backup evidence: %+v", report)
	}
}
