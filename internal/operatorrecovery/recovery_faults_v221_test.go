package operatorrecovery

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
)

func writeTestDB(t *testing.T, path string, script string) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if _, err = database.Exec(script); err != nil {
		t.Fatal(err)
	}
}

func pendingJournal(t *testing.T, directory string) {
	t.Helper()
	journal := model.RestoreJournal{BackupID: "backup-1", ArchivePath: "/tmp/archive.tar", State: "pending", CreatedAt: time.Now().UTC()}
	if err := os.WriteFile(filepath.Join(directory, "restore.pending.json"), []byte(journalPayload(t, journal)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationStatusFailureModes(t *testing.T) {
	t.Run("invalid instance config", func(t *testing.T) {
		var output bytes.Buffer
		if err := runMigrationStatus(commandOptions{}, &output); err == nil || !strings.Contains(err.Error(), "instance_validation") {
			t.Fatalf("status error = %v", err)
		}
	})
	t.Run("unreadable restore state", func(t *testing.T) {
		directory := privateTemp(t)
		if err := os.WriteFile(filepath.Join(directory, "restore.pending.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := collectMigrationStatus(t.Context(), Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")})
		if report.State != "failed" || report.Code != migrationCodeRestoreUnknown {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("lifecycle lock unavailable", func(t *testing.T) {
		directory := privateTemp(t)
		lockPath, err := applicationsecrets.LifecycleLockPath(directory)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(lockPath, 0o700); err != nil {
			t.Fatal(err)
		}
		report := collectMigrationStatus(t.Context(), Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")})
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("unreadable database entry", func(t *testing.T) {
		directory := privateTemp(t)
		if err := os.WriteFile(filepath.Join(directory, "beat.db"), []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db", "nested.db")}
		report := collectMigrationStatus(t.Context(), config)
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("pending restore without database", func(t *testing.T) {
		directory := privateTemp(t)
		pendingJournal(t, directory)
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
		report := collectMigrationStatus(t.Context(), config)
		if report.State != "restore_staged" || !report.PendingRestore {
			t.Fatalf("report = %+v", report)
		}
	})
}

func TestInspectMigrationDatabaseFailureModes(t *testing.T) {
	t.Run("staging unavailable", func(t *testing.T) {
		missing := filepath.Join(privateTemp(t), "absent", "private")
		config := Config{DataDir: missing, DatabasePath: filepath.Join(missing, "beat.db")}
		report := inspectMigrationDatabase(t.Context(), config, MigrationReport{})
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("database absent", func(t *testing.T) {
		directory := privateTemp(t)
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
		report := inspectMigrationDatabase(t.Context(), config, MigrationReport{})
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("not a database", func(t *testing.T) {
		directory := privateTemp(t)
		writeFaultFile(t, filepath.Join(directory, "beat.db"), []byte("not a database"))
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
		report := inspectMigrationDatabase(t.Context(), config, MigrationReport{})
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("broken ledger", func(t *testing.T) {
		directory := privateTemp(t)
		path := filepath.Join(directory, "beat.db")
		writeTestDB(t, path, "CREATE TABLE schema_migrations(version TEXT, name TEXT); INSERT INTO schema_migrations VALUES('abc','x')")
		report := inspectMigrationDatabase(t.Context(), Config{DataDir: directory, DatabasePath: path}, MigrationReport{})
		if report.State != "failed" || report.Code != migrationCodeUnreadable {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("pending restore", func(t *testing.T) {
		config := fixture(t)
		report := inspectMigrationDatabase(t.Context(), config, MigrationReport{PendingRestore: true})
		if report.State != "pending_restore" || report.Code != migrationCodePendingRestore {
			t.Fatalf("report = %+v", report)
		}
	})
}

func writeFaultFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectDatabaseFailureModes(t *testing.T) {
	t.Run("staging unavailable", func(t *testing.T) {
		missing := filepath.Join(privateTemp(t), "absent", "private")
		config := Config{DataDir: missing, DatabasePath: filepath.Join(missing, "beat.db")}
		if _, err := inspectDatabase(t.Context(), config); err == nil {
			t.Fatal("absent staging directory accepted")
		}
	})
	t.Run("database absent", func(t *testing.T) {
		directory := privateTemp(t)
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
		if _, err := inspectDatabase(t.Context(), config); err == nil {
			t.Fatal("absent database accepted")
		}
	})
}

func TestOpenRejectsUnsafeDataDirectory(t *testing.T) {
	directory := privateTemp(t)
	config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
	exposed := filepath.Join(directory, "exposed")
	if err := os.Mkdir(exposed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exposed, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("non-private entry accepted")
	}
}

func TestInspectInstanceFailureModes(t *testing.T) {
	t.Run("unreadable database entry", func(t *testing.T) {
		directory := privateTemp(t)
		writeFaultFile(t, filepath.Join(directory, "beat.db"), []byte("file"))
		config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db", "nested.db")}
		if _, err := inspectInstance(t.Context(), config); err == nil {
			t.Fatal("unsafe database entry accepted")
		}
	})
	t.Run("pending restore overrides actions", func(t *testing.T) {
		config := fixture(t)
		pendingJournal(t, config.DataDir)
		status, err := inspectInstance(t.Context(), config)
		if err != nil || !status.PendingRestore || len(status.NextActions) != 1 || status.NextActions[0] != "start-server" {
			t.Fatalf("status = %+v / %v", status, err)
		}
	})
}

func TestMigrationCheckHelperFailures(t *testing.T) {
	t.Run("filesystem probe unavailable", func(t *testing.T) {
		if _, err := checkFreeSpace(filepath.Join(privateTemp(t), "absent")); err == nil {
			t.Fatal("absent path accepted")
		}
	})
	t.Run("schema inventory unavailable", func(t *testing.T) {
		database, err := sql.Open("sqlite", "file:"+filepath.Join(privateTemp(t), "beat.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err = database.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = schemaInventory(t.Context(), database); err == nil {
			t.Fatal("closed database accepted")
		}
	})
	t.Run("archived integrity failure", func(t *testing.T) {
		path := filepath.Join(privateTemp(t), "broken.db")
		if _, err := inspectArchivedLedger(t.Context(), path); !errors.Is(err, errArchivedIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("archived ledger missing", func(t *testing.T) {
		path := filepath.Join(privateTemp(t), "empty.db")
		writeTestDB(t, path, "CREATE TABLE placeholder(id INTEGER)")
		if _, err := inspectArchivedLedger(t.Context(), path); !errors.Is(err, errArchivedLedger) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("error code mapping", func(t *testing.T) {
		if code := errCode(errArchivedLedger, "fallback"); code != migrationCodeLedgerInvalid {
			t.Fatalf("code = %s", code)
		}
		if code := errCode(errArchivedIntegrity, "fallback"); code != migrationCodeIntegrityFailed {
			t.Fatalf("code = %s", code)
		}
		if code := errCode(errors.New("other"), "fallback"); code != "fallback" {
			t.Fatalf("code = %s", code)
		}
	})
}
