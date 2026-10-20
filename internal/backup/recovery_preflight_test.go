package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSQLiteRejectsUnsupportedOrIncompleteLedger(t *testing.T) {
	for _, mutation := range []string{
		"INSERT INTO schema_migrations(version,name,applied_at) VALUES(9999,'future',CURRENT_TIMESTAMP)",
		"DELETE FROM schema_migrations WHERE version=2",
		"DELETE FROM schema_migrations",
	} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newBackupFixture(t)
			if _, err := fixture.sqlite.DB.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := validateSQLite(t.Context(), filepath.Join(fixture.dataDir, "beat.db")); err == nil {
				t.Fatal("invalid migration history was accepted for restore")
			}
		})
	}
}

func TestValidateSQLiteRejectsForeignKeyViolation(t *testing.T) {
	fixture := newBackupFixture(t)
	connection, err := fixture.sqlite.DB.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(t.Context(), "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(t.Context(), `INSERT INTO nodes(id,name,group_id,host,port,status,created_at,updated_at)
		VALUES('orphan','orphan','missing','127.0.0.1',22,'offline',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLite(t.Context(), filepath.Join(fixture.dataDir, "beat.db")); err == nil {
		t.Fatal("orphaned platform configuration was accepted")
	}
}

func TestApplyPendingRejectsFutureSchemaBeforeReplacingData(t *testing.T) {
	fixture := newBackupFixture(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != "ready" {
		t.Fatalf("prepare incompatible archive: %v", err)
	}
	archive := filepath.Join(fixture.dataDir, "backups", records[0].Filename)
	makeFutureSchemaArchive(t, archive)
	if err := writeJournal(filepath.Join(fixture.dataDir, "restore.pending.json"), restoreJournal(record.ID, archive)); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.dataDir, "beat_mts", "live-marker")
	if err := os.WriteFile(marker, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = ApplyPending(t.Context(), fixture.dataDir, filepath.Join(fixture.dataDir, "beat.db"), filepath.Join(fixture.dataDir, "beat_mts"))
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema preflight failure, got %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "must survive" {
		t.Fatal("restore replaced live metric state before schema validation")
	}
}
