package backup

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type backupFixture struct {
	dataDir string
	sqlite  *store.SQLiteStore
	mts     *store.MTSStore
	service *Service
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "beat.db")
	mtsPath := filepath.Join(dataDir, "beat_mts")
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new SQLite store: %v", err)
	}
	mtsStore, err := store.NewMTSStore(mtsPath)
	if err != nil {
		_ = sqliteStore.Close()
		t.Fatalf("new MTS store: %v", err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	if err := os.WriteFile(filepath.Join(dataDir, "admin-data.key"), key, 0o600); err != nil {
		t.Fatalf("write data key: %v", err)
	}
	records := store.NewBackupStore(sqliteStore.DB)
	service, err := NewService(t.Context(), records, sqliteStore, mtsStore,
		filepath.Join(dataDir, "backups"), filepath.Join(dataDir, "admin-data.key"), "test")
	if err != nil {
		_ = mtsStore.Close()
		_ = sqliteStore.Close()
		t.Fatalf("new backup service: %v", err)
	}
	fixture := &backupFixture{dataDir: dataDir, sqlite: sqliteStore, mts: mtsStore, service: service}
	t.Cleanup(func() {
		service.Wait()
		_ = mtsStore.Close()
		_ = sqliteStore.Close()
	})
	return fixture
}

func TestBackupCreateValidateUploadAndRestoreRoundTrip(t *testing.T) {
	fixture := newBackupFixture(t)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := fixture.mts.WriteMetric(t.Context(), "node-1", "cpu", 42.5, timestamp); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if err := fixture.mts.WriteNetworkProbes(t.Context(), []store.NetworkProbeSample{{
		TaskID: "task-1", NodeID: "node-1", TaskType: "tcp", FinishedAt: timestamp,
		LatencyMS: 12.5, Success: true, StatusCode: 200, ErrorCode: "none",
	}}); err != nil {
		t.Fatalf("write network metric: %v", err)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	records, err := fixture.service.List(t.Context())
	if err != nil || len(records) != 1 || records[0].State != "ready" || records[0].MetricRows != 2 {
		t.Fatalf("backup records = %#v, error = %v", records, err)
	}
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	uploaded, err := fixture.service.ValidateUpload(t.Context(), archive)
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("validate uploaded backup: %v, close: %v", err, closeErr)
	}
	if uploaded.State != "validated" || uploaded.MetricRows != 2 {
		t.Fatalf("uploaded record = %#v", uploaded)
	}

	targetData := t.TempDir()
	targetBackups := filepath.Join(targetData, "backups")
	if err := os.Mkdir(targetBackups, 0o700); err != nil {
		t.Fatalf("create target backup directory: %v", err)
	}
	sourcePath := filepath.Join(fixture.dataDir, "backups", uploaded.Filename)
	targetArchive := filepath.Join(targetBackups, uploaded.Filename)
	if err := copyFile(sourcePath, targetArchive, 0o600); err != nil {
		t.Fatalf("copy target archive: %v", err)
	}
	journal := restoreJournal(uploaded.ID, targetArchive)
	if err := writeJournal(filepath.Join(targetData, "restore.pending.json"), journal); err != nil {
		t.Fatalf("write restore journal: %v", err)
	}
	targetDB := filepath.Join(targetData, "beat.db")
	targetMTS := filepath.Join(targetData, "beat_mts")
	if err := ApplyPending(t.Context(), targetData, targetDB, targetMTS); err != nil {
		t.Fatalf("apply pending restore: %v", err)
	}
	restoredSQLite, err := store.NewSQLiteStore(targetDB)
	if err != nil {
		t.Fatalf("open restored SQLite: %v", err)
	}
	if err := restoredSQLite.Close(); err != nil {
		t.Fatalf("close restored SQLite: %v", err)
	}
	restoredMTS, err := store.NewMTSStore(targetMTS)
	if err != nil {
		t.Fatalf("open restored MTS: %v", err)
	}
	metrics, err := restoredMTS.QueryMetrics(t.Context(), []string{"cpu"},
		timestamp.Add(-time.Second), timestamp.Add(time.Second), "node-1")
	closeMTS := restoredMTS.Close()
	if err != nil || closeMTS != nil || len(metrics["cpu"]) != 1 || metrics["cpu"][0].Value != 42.5 {
		t.Fatalf("restored metrics = %#v, query = %v, close = %v", metrics, err, closeMTS)
	}
}

func TestValidateUploadRejectsTraversalAndRemovesArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	entry, err := writer.Create("../escape")
	if err != nil {
		t.Fatalf("create traversal entry: %v", err)
	}
	if _, err := io.WriteString(entry, "bad"); err != nil {
		t.Fatalf("write traversal entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close traversal archive: %v", err)
	}
	if _, err := fixture.service.ValidateUpload(t.Context(), bytes.NewReader(content.Bytes())); err == nil {
		t.Fatal("traversal archive validation error = nil")
	}
	entries, err := os.ReadDir(filepath.Join(fixture.dataDir, "backups"))
	if err != nil {
		t.Fatalf("read backup directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("backup directory entries = %d, want 0", len(entries))
	}
}

func TestBackupServiceStageDeleteAndErrorPaths(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.service.mu.Lock()
	fixture.service.running = true
	fixture.service.mu.Unlock()
	if _, err := fixture.service.Start(t.Context()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("overlapping backup error = %v", err)
	}
	fixture.service.mu.Lock()
	fixture.service.running = false
	fixture.service.mu.Unlock()
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	if _, err := fixture.service.StageRestore(t.Context(), record.ID, "wrong"); !errors.Is(err, ErrInvalidConfirm) {
		t.Fatalf("invalid restore confirmation error = %v", err)
	}
	staged, err := fixture.service.StageRestore(t.Context(), record.ID, RestoreConfirmation)
	if err != nil || staged.State != model.BackupStateStaged {
		t.Fatalf("stage restore = %#v, %v", staged, err)
	}
	journal, err := readJournal(filepath.Join(fixture.dataDir, "restore.pending.json"))
	if err != nil || journal.BackupID != record.ID || journal.State != "pending" {
		t.Fatalf("restore journal = %#v, %v", journal, err)
	}
	if err := fixture.service.Delete(t.Context(), record.ID); err == nil {
		t.Fatal("staged backup was deleted")
	}
	if _, _, err := fixture.service.Open(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing backup open error = %v", err)
	}
	if _, err := fixture.service.StageRestore(t.Context(), "missing", RestoreConfirmation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing restore error = %v", err)
	}
	if _, err := fixture.service.archivePath("../outside.zip"); err == nil {
		t.Fatal("unsafe archive filename accepted")
	}
	directory := filepath.Join(fixture.dataDir, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := removeRegularFile(directory); err == nil {
		t.Fatal("directory removed as backup file")
	}
	if _, err := NewService(t.Context(), nil, fixture.sqlite, fixture.mts, "", "", ""); err == nil {
		t.Fatal("missing backup dependencies accepted")
	}
	temporary, err := os.CreateTemp(fixture.dataDir, "bounded-")
	if err != nil {
		t.Fatalf("create bounded destination: %v", err)
	}
	if _, err := copyBounded(temporary, strings.NewReader("too large"), 2); err == nil {
		t.Fatal("oversized bounded copy accepted")
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("close bounded destination: %v", err)
	}
}

func TestArchiveValidationRejectsUnsafeMetadataAndPayloads(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "unsafe.zip")
	writeZipEntries(t, archivePath, []zipTestEntry{
		{name: manifestEntry, content: "{}"},
		{name: sqliteEntry, content: "sqlite"},
		{name: metricsEntry, content: "metrics"},
		{name: checksumsEntry, content: "checksums"},
		{name: "unknown", content: "bad"},
	})
	if _, err := validateArchive(t.Context(), archivePath, root); err == nil {
		t.Fatal("unknown archive entry accepted")
	}
	writeZipEntries(t, archivePath, []zipTestEntry{
		{name: manifestEntry, content: "{}"}, {name: manifestEntry, content: "{}"},
		{name: sqliteEntry, content: "sqlite"}, {name: metricsEntry, content: "metrics"},
		{name: checksumsEntry, content: "checksums"},
	})
	if _, err := validateArchive(t.Context(), archivePath, root); err == nil {
		t.Fatal("duplicate archive entry accepted")
	}

	invalidManifest := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(invalidManifest, []byte(`{"format_version":2}`), 0o600); err != nil {
		t.Fatalf("write invalid manifest: %v", err)
	}
	if _, err := readManifest(invalidManifest); err == nil {
		t.Fatal("incompatible manifest accepted")
	}
	if err := os.WriteFile(filepath.Join(root, checksumsEntry), []byte("malformed"), 0o600); err != nil {
		t.Fatalf("write checksum list: %v", err)
	}
	if err := validateChecksumList(root, model.BackupManifest{Checksums: map[string]string{"x": "y"}}); err == nil {
		t.Fatal("malformed checksum list accepted")
	}
	badSQLite := filepath.Join(root, "bad.sqlite")
	if err := os.WriteFile(badSQLite, []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write bad SQLite: %v", err)
	}
	if err := validateSQLite(t.Context(), badSQLite); err == nil {
		t.Fatal("invalid SQLite accepted")
	}
	badMetrics := filepath.Join(root, "bad.jsonl.gz")
	writeMetricLines(t, badMetrics, []string{`{"measurement":"unknown"}`})
	if _, err := validateMetrics(badMetrics, time.Now().UnixNano()); err == nil {
		t.Fatal("invalid metric row accepted")
	}
	if validMetricRecord(metricRecord{}, time.Now().UnixNano()) {
		t.Fatal("empty metric record accepted")
	}
	valid := metricRecord{Measurement: "cpu", Tags: map[string]string{"node": "node"},
		TimestampNS: time.Now().Add(-time.Second).UnixNano(),
		Fields:      map[string]mts.FieldValue{"value": mts.Float64Value(1)}}
	if !validMetricRecord(valid, time.Now().UnixNano()) || !knownMetric("net_recv_delta") {
		t.Fatal("valid metric record rejected")
	}
}

func TestApplyPendingIgnoresAbsentOrAppliedJournalAndRejectsOutsideArchive(t *testing.T) {
	root := t.TempDir()
	applier := NewApplier()
	if err := applier.ApplyPending(t.Context(), root, filepath.Join(root, "db"), filepath.Join(root, "mts")); err != nil {
		t.Fatalf("absent journal: %v", err)
	}
	applied := restoreJournal("backup", filepath.Join(root, "backups", "backup.zip"))
	applied.State = "applied"
	if err := writeJournal(filepath.Join(root, "restore.pending.json"), applied); err != nil {
		t.Fatalf("write applied journal: %v", err)
	}
	if err := applier.ApplyPending(t.Context(), root, filepath.Join(root, "db"), filepath.Join(root, "mts")); err != nil {
		t.Fatalf("applied journal: %v", err)
	}
	pending := restoreJournal("backup", filepath.Join(root, "outside.zip"))
	if err := writeJournal(filepath.Join(root, "restore.pending.json"), pending); err != nil {
		t.Fatalf("write outside journal: %v", err)
	}
	if err := applier.ApplyPending(t.Context(), root, filepath.Join(root, "db"), filepath.Join(root, "mts")); err == nil {
		t.Fatal("outside restore archive accepted")
	}
}

func TestSwapRollsBackEveryRenameBoundary(t *testing.T) {
	for failureAt := 1; failureAt <= 4; failureAt++ {
		t.Run(string(rune('0'+failureAt)), func(t *testing.T) {
			root := t.TempDir()
			prepared := createSwapFixture(t, root)
			calls := 0
			applier := &Applier{rename: func(oldPath, newPath string) error {
				calls++
				if calls == failureAt {
					return context.Canceled
				}
				return os.Rename(oldPath, newPath)
			}, now: time.Now}
			if _, err := applier.swap(prepared, "test"); err == nil {
				t.Fatal("swap error = nil")
			}
			assertFileContent(t, prepared.targets[0].live, "old-db")
			assertFileContent(t, filepath.Join(prepared.targets[1].live, "marker"), "old-mts")
		})
	}
}

func createSwapFixture(t *testing.T, root string) *preparedRestore {
	t.Helper()
	dbLive := filepath.Join(root, "beat.db")
	dbNew := filepath.Join(root, "beat.db.new")
	mtsLive := filepath.Join(root, "mts")
	mtsNew := filepath.Join(root, "mts.new")
	writeTestFile(t, dbLive, "old-db")
	writeTestFile(t, dbNew, "new-db")
	if err := os.Mkdir(mtsLive, 0o700); err != nil {
		t.Fatalf("create old MTS: %v", err)
	}
	if err := os.Mkdir(mtsNew, 0o700); err != nil {
		t.Fatalf("create new MTS: %v", err)
	}
	writeTestFile(t, filepath.Join(mtsLive, "marker"), "old-mts")
	writeTestFile(t, filepath.Join(mtsNew, "marker"), "new-mts")
	return &preparedRestore{targets: []restoreTarget{{live: dbLive, new: dbNew}, {live: mtsLive, new: mtsNew}}}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != expected {
		t.Fatalf("read %s = %q, error = %v, want %q", path, content, err, expected)
	}
}

func restoreJournal(id, path string) model.RestoreJournal {
	return model.RestoreJournal{BackupID: id, ArchivePath: path, State: "pending", CreatedAt: time.Now().UTC()}
}

type zipTestEntry struct {
	name    string
	content string
}

func writeZipEntries(t *testing.T, path string, entries []zipTestEntry) {
	t.Helper()
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create ZIP: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		destination, createErr := writer.Create(entry.name)
		if createErr != nil {
			t.Fatalf("create ZIP entry: %v", createErr)
		}
		if _, writeErr := io.WriteString(destination, entry.content); writeErr != nil {
			t.Fatalf("write ZIP entry: %v", writeErr)
		}
	}
	if err := errors.Join(writer.Close(), file.Close()); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
}

func writeMetricLines(t *testing.T, path string, lines []string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create metric gzip: %v", err)
	}
	writer := gzip.NewWriter(file)
	buffer := bufio.NewWriter(writer)
	for _, line := range lines {
		if _, err := buffer.WriteString(line + "\n"); err != nil {
			t.Fatalf("write metric line: %v", err)
		}
	}
	if err := errors.Join(buffer.Flush(), writer.Close(), file.Close()); err != nil {
		t.Fatalf("close metric gzip: %v", err)
	}
}
