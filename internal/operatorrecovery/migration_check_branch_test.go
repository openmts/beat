package operatorrecovery

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestRunMigrationCheckReportsCommandFailures(t *testing.T) {
	var output bytes.Buffer
	if err := runMigrationCheck(commandOptions{}, &output); err == nil ||
		!strings.Contains(err.Error(), "instance_validation") {
		t.Fatalf("empty config = %v", err)
	}
	exposed := privateTemp(t)
	leaked := filepath.Join(exposed, "world-readable.txt")
	if err := os.WriteFile(leaked, []byte("leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	// WriteFile 的权限位会被进程 umask 削减（scripts/check.sh 设了 umask 077），
	// 所以这里显式 Chmod：用例要的是「磁盘上真的存在一个他人可读的条目」。
	if err := os.Chmod(leaked, 0o644); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runMigrationCheck(commandOptions{config: Config{DataDir: exposed}}, &output); err == nil ||
		!strings.Contains(err.Error(), "private_directory") {
		t.Fatalf("exposed entry = %v", err)
	}
}

func TestCheckMigrationArchiveRecordsFailureMarker(t *testing.T) {
	config, archive := checkFixture(t)
	writer := migrationstatus.NewWriter(config.DataDir, "0.2.0-dev", "check-build")
	if err := writer.Failed(store.MigrationFailureApply, true); err != nil {
		t.Fatal(err)
	}
	report := checkMigrationArchive(t.Context(), config, archive)
	if report.LastFailure == nil || report.LastFailure.State != migrationstatus.StateFailed ||
		!report.LastFailure.RollbackRequired || report.LastFailure.FailureCode != store.MigrationFailureApply {
		t.Fatalf("last failure = %+v", report.LastFailure)
	}
	if !report.Eligible || report.Code != migrationCodeOK {
		t.Fatalf("report = %+v", report)
	}
}

func TestCheckMigrationArchiveRejectsPendingRestoreStates(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		expected string
	}{
		{name: "pending", expected: migrationCodePendingRestore, payload: journalPayload(t, model.RestoreJournal{
			BackupID: "backup-1", ArchivePath: "/tmp/archive.tar", State: "pending", CreatedAt: time.Now().UTC()})},
		{name: "unknown", expected: migrationCodeRestoreUnknown, payload: `{"backup_id":"broken"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := privateTemp(t)
			if err := os.WriteFile(filepath.Join(directory, "restore.pending.json"), []byte(test.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			report := checkMigrationArchive(t.Context(), Config{DataDir: directory}, filepath.Join(directory, "absent.zip"))
			if report.Code != test.expected || report.Eligible {
				t.Fatalf("report = %+v", report)
			}
			if test.expected == migrationCodePendingRestore && !report.PendingRestore {
				t.Fatalf("pending restore not reported: %+v", report)
			}
		})
	}
}

func TestCheckMigrationArchiveRejectsLifecycleLockFailure(t *testing.T) {
	directory := privateTemp(t)
	config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
	lockPath, err := applicationsecrets.LifecycleLockPath(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	report := checkMigrationArchive(t.Context(), config, filepath.Join(directory, "absent.zip"))
	if report.Code != migrationCodeUnreadable || report.Eligible {
		t.Fatalf("report = %+v", report)
	}
}

func TestCheckMigrationArchiveRejectsInsufficientSpace(t *testing.T) {
	directory := privateTemp(t)
	sparse := filepath.Join(directory, "sparse.zip")
	file, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(300 << 30); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	config := Config{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
	report := checkMigrationArchive(t.Context(), config, sparse)
	if report.Code != checkCodeInsufficientRoom || report.Eligible {
		t.Fatalf("report = %+v", report)
	}
	if report.RequiredFreeBytes <= report.FreeSpaceBytes {
		t.Fatalf("space accounting = %+v", report)
	}
}

func TestCheckMigrationArchiveRequiresLiveDatabaseFingerprint(t *testing.T) {
	config, archive := checkFixture(t)
	if err := os.Remove(config.DatabasePath); err != nil {
		t.Fatal(err)
	}
	report := checkMigrationArchive(t.Context(), config, archive)
	if report.LiveDatabasePresent || report.Eligible || report.Code != migrationCodeUnreadable {
		t.Fatalf("report = %+v", report)
	}
}

func journalPayload(t *testing.T, journal model.RestoreJournal) string {
	t.Helper()
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
