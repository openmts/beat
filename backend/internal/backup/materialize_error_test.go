package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestArchiveCreationHelperErrors(t *testing.T) {
	root := t.TempDir()
	service := &Service{root: root, now: time.Now}
	service.dataKeyPath = filepath.Join(root, "missing-key")
	if err := service.copyDataKey(t.TempDir()); err != nil {
		t.Fatalf("missing optional data key: %v", err)
	}
	shortKey := filepath.Join(root, "short-key")
	if err := os.WriteFile(shortKey, bytes.Repeat([]byte{1}, 31), 0o600); err != nil {
		t.Fatalf("write short key: %v", err)
	}
	service.dataKeyPath = shortKey
	if err := service.copyDataKey(t.TempDir()); err == nil {
		t.Fatal("short administrator data key copied")
	}
	service.dataKeyPath = root
	if err := service.copyDataKey(t.TempDir()); err == nil {
		t.Fatal("directory copied as administrator data key")
	}
	if _, err := service.buildManifest(root, []string{"missing"}, time.Now(), 0); err == nil {
		t.Fatal("manifest built with missing payload")
	}
	if _, _, err := fileDigest(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing payload checksum succeeded")
	}
}

func TestArchiveMetadataAndPublishErrors(t *testing.T) {
	root := t.TempDir()
	manifest := model.BackupManifest{Checksums: map[string]string{}, PayloadSizes: map[string]int64{}}
	parentFile := filepath.Join(root, "parent")
	writeTestFile(t, parentFile, "file")
	if err := writeMetadataFiles(filepath.Join(parentFile, "metadata"), manifest); err == nil {
		t.Fatal("metadata written below regular file")
	}
	if err := writeArchive(filepath.Join(parentFile, "partial"), filepath.Join(root, "archive.zip"), root, nil); err == nil {
		t.Fatal("archive created below regular file")
	}
	writeTestFile(t, filepath.Join(root, manifestEntry), "manifest")
	writeTestFile(t, filepath.Join(root, checksumsEntry), "checksums")
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	if err := writeArchive(filepath.Join(root, "partial.zip"), destination, root, nil); err == nil {
		t.Fatal("archive replaced a directory")
	}
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	if err := addArchiveEntries(writer, root, []string{"missing"}); err == nil {
		t.Fatal("missing archive payload added")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close in-memory ZIP: %v", err)
	}
	if backupFilename(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "12345678-rest") !=
		"beat-backup-v1-20260102T030405Z-12345678.zip" {
		t.Fatal("backup filename format is incorrect")
	}
	if !errors.Is(errorsJoin(context.Canceled, nil), context.Canceled) {
		t.Fatal("backup error join lost the source error")
	}
}

func TestCreateArchiveAndMetricsFileErrors(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "root")
	writeTestFile(t, parentFile, "file")
	service := &Service{root: parentFile, now: time.Now}
	if _, err := service.createArchive(t.Context(), model.BackupRecord{Filename: "backup.zip"}); err == nil {
		t.Fatal("backup workspace created below regular file")
	}
	metricsPath := filepath.Join(t.TempDir(), "metrics.jsonl.gz")
	writeTestFile(t, metricsPath, "existing")
	if _, err := service.writeMetrics(t.Context(), metricsPath, time.Now()); err == nil {
		t.Fatal("MTS export replaced an existing file")
	}
}

func TestRestoreMaterializationErrors(t *testing.T) {
	root := t.TempDir()
	validated := &validatedArchive{root: root}
	if _, err := prepareRestore(validated, filepath.Join(root, "db"), filepath.Join(root, "mts"), root, "token"); err == nil {
		t.Fatal("restore prepared without SQLite payload")
	}
	metrics := filepath.Join(root, "metrics")
	if err := importMetrics(t.Context(), metrics, root); err == nil {
		t.Fatal("restored MTS replaced an existing directory")
	}
	if err := replayMetrics(t.Context(), filepath.Join(root, "missing"), nil); err == nil {
		t.Fatal("missing MTS export replayed")
	}
	plain := filepath.Join(root, "plain.gz")
	writeTestFile(t, plain, "plain")
	if err := replayMetrics(t.Context(), plain, nil); err == nil {
		t.Fatal("invalid MTS gzip replayed")
	}
	badJSON := filepath.Join(root, "bad.jsonl.gz")
	writeMetricLines(t, badJSON, []string{"{"})
	destination, err := newTestMTS(t)
	if err != nil {
		t.Fatalf("new replay MTS: %v", err)
	}
	if err := replayMetrics(t.Context(), badJSON, destination); err == nil {
		t.Fatal("invalid MTS row replayed")
	}
	if err := validateApplied(t.Context(), filepath.Join(root, "missing.db"), filepath.Join(root, "mts")); err == nil {
		t.Fatal("invalid applied restore validated")
	}
}

func TestCopyFileAndPreparedCleanup(t *testing.T) {
	root := t.TempDir()
	if err := copyFile(filepath.Join(root, "missing"), filepath.Join(root, "target"), 0o600); err == nil {
		t.Fatal("missing restore source copied")
	}
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	writeTestFile(t, source, "source")
	writeTestFile(t, destination, "destination")
	if err := copyFile(source, destination, 0o600); err == nil {
		t.Fatal("restore copy replaced an existing target")
	}
	cleanupPath := filepath.Join(root, "cleanup")
	writeTestFile(t, cleanupPath, "new")
	prepared := &preparedRestore{targets: []restoreTarget{{new: cleanupPath}}}
	prepared.cleanup()
	if _, err := os.Stat(cleanupPath); !os.IsNotExist(err) {
		t.Fatalf("prepared restore cleanup error = %v", err)
	}
	committedPath := filepath.Join(root, "committed")
	writeTestFile(t, committedPath, "new")
	prepared = &preparedRestore{committed: true, targets: []restoreTarget{{new: committedPath}}}
	prepared.cleanup()
	if _, err := os.Stat(committedPath); err != nil {
		t.Fatalf("committed restore target removed: %v", err)
	}
}

func TestRollbackAppliedRestoresOldTargets(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "beat.db")
	newPath := live + ".new"
	rollback := live + ".rollback"
	mtsLive := filepath.Join(root, "mts")
	mtsRollback := filepath.Join(root, "mts.rollback")
	writeTestFile(t, live, "restored")
	writeTestFile(t, rollback, "original")
	if err := os.Mkdir(mtsLive, 0o700); err != nil {
		t.Fatalf("create restored MTS: %v", err)
	}
	if err := os.Mkdir(mtsRollback, 0o700); err != nil {
		t.Fatalf("create rollback MTS: %v", err)
	}
	writeTestFile(t, filepath.Join(mtsRollback, "marker"), "original-mts")
	prepared := &preparedRestore{mtsLive: mtsLive, targets: []restoreTarget{{live: live, new: newPath}}}
	rollbacks := map[string]string{live: rollback, mtsLive: mtsRollback}
	if err := NewApplier().rollbackApplied(prepared, rollbacks); err != nil {
		t.Fatalf("rollback applied restore: %v", err)
	}
	assertFileContent(t, live, "original")
	assertFileContent(t, filepath.Join(mtsLive, "marker"), "original-mts")
	assertFileContent(t, newPath, "restored")
}

func TestArchiveValidationStagingAndExtractionErrors(t *testing.T) {
	fixture := newBackupFixture(t)
	archive, record := generatedArchive(t, fixture)
	archivePath := filepath.Join(t.TempDir(), record.Filename)
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("write validation archive: %v", err)
	}
	stagingFile := filepath.Join(t.TempDir(), "staging")
	writeTestFile(t, stagingFile, "file")
	if _, err := validateArchive(t.Context(), archivePath, stagingFile); err == nil {
		t.Fatal("validation staging directory created below regular file")
	}
	files := fakeZipFiles(zip.FileHeader{Name: manifestEntry, Method: zip.Store})
	files[0].UncompressedSize64 = uint64(MaximumExpandedBytes) + 1
	if err := extractAllowlisted(files, t.TempDir()); err == nil {
		t.Fatal("oversized expanded archive accepted")
	}
}

func TestExtractedArchiveAndChecksumErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := validateExtracted(t.Context(), root); err == nil {
		t.Fatal("extracted archive without manifest validated")
	}
	manifest := model.BackupManifest{FormatVersion: 1, CreatedAt: time.Now(), SnapshotCutoff: time.Now(),
		PayloadSizes: map[string]int64{}, Checksums: map[string]string{}}
	if err := writeManifestOnly(root, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := validateExtracted(t.Context(), root); err == nil {
		t.Fatal("extracted archive without payloads validated")
	}
	keyPath := filepath.Join(root, filepath.FromSlash(dataKeyEntry))
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatalf("create key directory: %v", err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{1}, 32), 0o600); err != nil {
		t.Fatalf("write undeclared key: %v", err)
	}
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("undeclared administrator key accepted")
	}
	if err := validateChecksumList(t.TempDir(), manifest); err == nil {
		t.Fatal("missing checksum list accepted")
	}
	writeTestFile(t, filepath.Join(root, checksumsEntry), "sum  platform.sqlite\n")
	manifest.Checksums = map[string]string{sqliteEntry: "other"}
	if err := validateChecksumList(root, manifest); err == nil {
		t.Fatal("mismatched checksum accepted")
	}
}

func TestSQLiteAndMetricSchemaValidation(t *testing.T) {
	root := t.TempDir()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(root, "minimal.db"))
	if err != nil {
		t.Fatalf("new validation SQLite: %v", err)
	}
	if _, err := sqliteStore.DB.Exec("DROP TABLE admin_audit_events"); err != nil {
		t.Fatalf("drop required table: %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close validation SQLite: %v", err)
	}
	if err := validateSQLite(t.Context(), filepath.Join(root, "minimal.db")); err == nil {
		t.Fatal("SQLite snapshot missing required table validated")
	}
	if _, err := validateMetrics(filepath.Join(root, "missing.metrics"), time.Now().UnixNano()); err == nil {
		t.Fatal("missing metrics export validated")
	}
	network := metricRecord{Measurement: "network_probe", TimestampNS: time.Now().Add(-time.Second).UnixNano(),
		Tags: map[string]string{"task": "task", "node": "node", "type": "tcp"},
		Fields: map[string]mts.FieldValue{"latency_ms": mts.Float64Value(1), "success": mts.Float64Value(1),
			"status_code": mts.Int64Value(200), "error_code": mts.StringValue("")}}
	if !validMetricRecord(network, time.Now().UnixNano()) {
		t.Fatal("valid network probe metric rejected")
	}
	if exactKeys(map[string]string{"node": "node", "extra": "value"}, "node") {
		t.Fatal("metric with extra tag accepted")
	}
	if exactFields(map[string]mts.FieldValue{}, map[string]mts.FieldType{"value": mts.FieldFloat64}) {
		t.Fatal("metric with missing field accepted")
	}
}

func writeManifestOnly(root string, manifest model.BackupManifest) error {
	content, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, manifestEntry), content, 0o600)
}

func newTestMTS(t *testing.T) (*store.MTSStore, error) {
	t.Helper()
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err == nil {
		t.Cleanup(func() { _ = mtsStore.Close() })
	}
	return mtsStore, err
}
