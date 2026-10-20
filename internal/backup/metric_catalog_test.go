package backup

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func customHistoryBackup(t *testing.T) *backupFixture {
	t.Helper()
	fixture := newBackupFixture(t)
	customs := store.NewCustomMetricStore(fixture.sqlite.DB)
	metric, err := customs.Create(t.Context(), model.CustomMetric{Name: "deleted_history", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.mts.WriteMetric(t.Context(), "historic-node", "custom_deleted_history", 7, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := customs.Delete(t.Context(), metric.ID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func backupCatalogManifest(t *testing.T, path string) model.BackupManifest {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	for _, entry := range archive.File {
		if entry.Name != manifestEntry {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		var manifest model.BackupManifest
		decodeErr := json.NewDecoder(reader).Decode(&manifest)
		closeErr := reader.Close()
		if decodeErr != nil || closeErr != nil {
			t.Fatalf("manifest read: %v/%v", decodeErr, closeErr)
		}
		return manifest
	}
	t.Fatal("manifest is missing")
	return model.BackupManifest{}
}

func TestBackupV3IncludesDeletedCustomHistoryAndRestoresIt(t *testing.T) {
	fixture := customHistoryBackup(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateReady || records[0].MetricRows != 1 {
		t.Fatalf("custom archive=%v/%v", records, err)
	}
	source := filepath.Join(fixture.service.root, record.Filename)
	manifest := backupCatalogManifest(t, source)
	if manifest.FormatVersion != 4 {
		t.Fatalf("catalog archive format=%d", manifest.FormatVersion)
	}
	destination := t.TempDir()
	archiveRoot := filepath.Join(destination, "backups")
	if err := os.Mkdir(archiveRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveRoot, record.Filename)
	if err := copyFile(source, archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(filepath.Join(destination, "restore.pending.json"), restoreJournal(record.ID, archivePath)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(t.Context(), destination, filepath.Join(destination, "beat.db"), filepath.Join(destination, "mts")); err != nil {
		t.Fatal(err)
	}
	restored, err := store.NewMTSStore(filepath.Join(destination, "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	now := time.Now().UTC()
	points, err := restored.QueryMetrics(t.Context(), []string{"custom_deleted_history"}, now.Add(-time.Hour), now, "historic-node")
	if err != nil || len(points["custom_deleted_history"]) != 1 || points["custom_deleted_history"][0].Value != 7 {
		t.Fatal("restore omitted retired custom history")
	}
}
