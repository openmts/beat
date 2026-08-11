package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestBackupStoreLifecycle(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := model.NowUTC()
	record := model.BackupRecord{ID: "backup", Filename: "backup.zip", Source: "generated",
		State: model.BackupStateRunning, CreatedAt: now}
	if err := store.Create(t.Context(), &record); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if err := store.Create(t.Context(), &record); err == nil {
		t.Fatal("duplicate backup record succeeded")
	}
	loaded, err := store.Get(t.Context(), record.ID)
	if err != nil || loaded == nil || loaded.Filename != record.Filename {
		t.Fatalf("loaded backup = %#v, %v", loaded, err)
	}
	completed := now.Add(time.Minute)
	record.State = model.BackupStateReady
	record.CompletedAt = &completed
	record.SnapshotCutoff = &now
	record.SizeBytes = 100
	record.MetricRows = 5
	if err := store.Update(t.Context(), &record); err != nil {
		t.Fatalf("update backup: %v", err)
	}
	records, err := store.List(t.Context())
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateReady {
		t.Fatalf("backup records = %#v, %v", records, err)
	}
	if missing, err := store.Get(t.Context(), "missing"); err != nil || missing != nil {
		t.Fatalf("missing backup = %#v, %v", missing, err)
	}
	if err := store.Delete(t.Context(), record.ID); err != nil {
		t.Fatalf("delete backup: %v", err)
	}
	if err := store.Delete(t.Context(), record.ID); err == nil {
		t.Fatal("missing backup delete error = nil")
	}
	if err := store.Update(t.Context(), &record); err == nil {
		t.Fatal("missing backup update error = nil")
	}
}
