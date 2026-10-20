package operatorrecovery

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

// checkFixture 在私有数据目录里生成一份真实归档，用于只读迁移预检。
func checkFixture(t *testing.T) (Config, string) {
	t.Helper()
	config := Config{DataDir: privateTemp(t)}
	config.DatabasePath = filepath.Join(config.DataDir, "beat.db")
	sqliteStore, err := store.NewSQLiteStore(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	mtsStore, err := store.NewMTSStore(filepath.Join(config.DataDir, "beat_mts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.DataDir, "admin-data.key"), bytes.Repeat([]byte{7}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := testutil.OpenSecrets(t, sqliteStore.DB, config.DataDir)
	service, err := backup.NewService(t.Context(), store.NewBackupStore(sqliteStore.DB), sqliteStore, mtsStore,
		filepath.Join(config.DataDir, "backups"), filepath.Join(config.DataDir, "admin-data.key"), "test")
	if err != nil {
		t.Fatal(err)
	}
	service.SetSecretLifecycle(secrets)
	record, err := service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	service.Wait()
	secrets.Close()
	if err := mtsStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}
	return config, filepath.Join(config.DataDir, "backups", record.Filename)
}

func decodeCheckReport(t *testing.T, output *bytes.Buffer) MigrationCheckReport {
	t.Helper()
	var report MigrationCheckReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode report %q: %v", output.String(), err)
	}
	return report
}

func TestMigrationCheckAcceptsMatchingArchive(t *testing.T) {
	config, archive := checkFixture(t)
	before, err := os.ReadFile(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"migration-check", "--data-dir", config.DataDir, "--backup", archive}, &output); err != nil {
		t.Fatalf("check failed: %v (%s)", err, output.String())
	}
	report := decodeCheckReport(t, &output)
	if !report.Eligible || report.State != "eligible" || report.Code != migrationCodeOK {
		t.Fatalf("report: %+v", report)
	}
	if report.SourceSchema != store.MaximumSupportedSchemaVersion || report.TargetSchema != store.MaximumSupportedSchemaVersion {
		t.Fatalf("schema range: %+v", report)
	}
	if report.ArchiveFormat != 4 || !report.ArchivePresent || !report.LiveDatabasePresent {
		t.Fatalf("archive metadata: %+v", report)
	}
	if !report.Integrity || !report.ForeignKeys || !report.LedgerValid || !report.SchemaEquivalent {
		t.Fatalf("schema validation: %+v", report)
	}
	if !report.LiveDatabaseUnchanged || !report.StagingRemoved || report.LockConflict || report.PendingRestore {
		t.Fatalf("safety flags: %+v", report)
	}
	if len(report.PendingMigrations) != 0 || report.FreeSpaceBytes < report.RequiredFreeBytes {
		t.Fatalf("capacity: %+v", report)
	}
	after, err := os.ReadFile(config.DatabasePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("migration-check changed the live database")
	}
	entries, err := os.ReadDir(filepath.Dir(config.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len(checkStagingPrefix) && entry.Name()[:len(checkStagingPrefix)] == checkStagingPrefix {
			t.Fatalf("staging %q was not removed", entry.Name())
		}
	}
}

func TestMigrationCheckRejectsInvalidArchives(t *testing.T) {
	directory := privateTemp(t)
	for name, args := range map[string][]string{
		"missing archive": {"migration-check", "--data-dir", directory, "--backup", filepath.Join(directory, "absent.zip")},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := Run(args, &output); err == nil {
				t.Fatal("missing archive accepted")
			}
			if report := decodeCheckReport(t, &output); report.Eligible || report.Code != checkCodeArchiveMissing {
				t.Fatalf("report: %+v", report)
			}
		})
	}
	garbage := filepath.Join(privateTemp(t), "archive.zip")
	if err := os.WriteFile(garbage, []byte("not a zip archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"migration-check", "--data-dir", directory, "--backup", garbage}, &output); err == nil {
		t.Fatal("invalid archive accepted")
	}
	if report := decodeCheckReport(t, &output); report.Eligible || report.Code != checkCodeArchiveInvalid || report.ArchivePresent == false {
		t.Fatalf("report: %+v", report)
	}
	if err := Run([]string{"migration-check", "--data-dir", directory}, &output); err == nil {
		t.Fatal("missing --backup accepted")
	}
}

func TestMigrationCheckReportsLockConflict(t *testing.T) {
	directory := privateTemp(t)
	archive := filepath.Join(directory, "archive.zip")
	if err := os.WriteFile(archive, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease, err := applicationsecrets.AcquireLease(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Close() }()
	var output bytes.Buffer
	if err := Run([]string{"migration-check", "--data-dir", directory, "--backup", archive}, &output); err == nil {
		t.Fatal("running server accepted")
	}
	report := decodeCheckReport(t, &output)
	if !report.LockConflict || report.State != "locked" || report.Code != migrationCodeLockConflict {
		t.Fatalf("report: %+v", report)
	}
}

func TestRequiredCheckFreeBytesIsBounded(t *testing.T) {
	if requiredCheckFreeBytes(0) != minimumCheckFreeBytes {
		t.Fatal("zero-size floor")
	}
	if requiredCheckFreeBytes(1024) != 2*1024+minimumCheckFreeBytes {
		t.Fatal("archive doubling")
	}
}
