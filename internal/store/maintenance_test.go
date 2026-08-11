package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestMaintenanceSettingsStoreLifecycle(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewMaintenanceSettingsStore(sqliteStore.DB)
	settings, status, err := settingsStore.Get(t.Context())
	if err != nil || settings.RetentionDays != 30 || status.LastStatus != model.MaintenanceStatusNever {
		t.Fatalf("initial settings = %+v, status = %+v, err = %v", settings, status, err)
	}

	settings.RetentionDays = 90
	settings.AutoCleanupEnabled = false
	settings.CleanupHourUTC = 8
	if _, err := settingsStore.Update(t.Context(), settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	started := model.NowUTC().Add(-time.Second)
	cutoff := started.Add(-90 * 24 * time.Hour)
	if err := settingsStore.MarkStarted(t.Context(), model.MaintenanceTriggerManual, started, cutoff); err != nil {
		t.Fatalf("mark started: %v", err)
	}
	if err := settingsStore.MarkCompleted(t.Context(), model.NowUTC(), time.Second,
		model.MaintenanceStatusSuccess, "", "ok"); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	settings, status, err = settingsStore.Get(t.Context())
	if err != nil || settings.RetentionDays != 90 || status.Running ||
		status.LastStatus != model.MaintenanceStatusSuccess || status.SQLiteIntegrity != "ok" {
		t.Fatalf("saved settings = %+v, status = %+v, err = %v", settings, status, err)
	}
}

func TestMaintenanceSettingsStoreRejectsInvalidSettings(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewMaintenanceSettingsStore(sqliteStore.DB)
	_, err := settingsStore.Update(t.Context(), model.MaintenanceSettings{})
	if err == nil {
		t.Fatal("update invalid settings error = nil")
	}
}

func TestMaintenanceSettingsStoreDatabaseErrors(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewMaintenanceSettingsStore(sqliteStore.DB)
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close SQLite store: %v", err)
	}
	now := model.NowUTC()
	if _, _, err := settingsStore.Get(t.Context()); err == nil {
		t.Fatal("get closed database error = nil")
	}
	if _, err := settingsStore.Update(t.Context(), model.DefaultMaintenanceSettings()); err == nil {
		t.Fatal("update closed database error = nil")
	}
	if err := settingsStore.MarkStarted(t.Context(), model.MaintenanceTriggerManual, now, now); err == nil {
		t.Fatal("mark started closed database error = nil")
	}
	if err := settingsStore.MarkCompleted(t.Context(), now, time.Second,
		model.MaintenanceStatusSuccess, "", "ok"); err == nil {
		t.Fatal("mark completed closed database error = nil")
	}
	if err := settingsStore.RecoverInterrupted(t.Context()); err == nil {
		t.Fatal("recover closed database error = nil")
	}
}

func TestMaintenanceSettingsStoreRecoversInterruptedRun(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewMaintenanceSettingsStore(sqliteStore.DB)
	now := model.NowUTC()
	if err := settingsStore.MarkStarted(t.Context(), model.MaintenanceTriggerAutomatic, now, now); err != nil {
		t.Fatalf("mark started: %v", err)
	}
	if err := settingsStore.RecoverInterrupted(t.Context()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	_, status, err := settingsStore.Get(t.Context())
	if err != nil || status.Running || status.LastStatus != model.MaintenanceStatusFailed ||
		status.LastError != interruptedMaintenanceMessage {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}

func TestSQLiteMaintenancePreservesApplicationData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beat.db")
	sqliteStore, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	if _, err := sqliteStore.DB.ExecContext(t.Context(), `INSERT INTO alert_events
		(id, rule_id, node_id, message, value, status, triggered_at)
		VALUES ('event', 'rule', 'node', 'kept', 1, 'active', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("insert application row: %v", err)
	}
	integrity, err := sqliteStore.Maintain(t.Context())
	if err != nil || integrity != "ok" {
		t.Fatalf("maintain SQLite: integrity = %q, err = %v", integrity, err)
	}
	var count int
	if err := sqliteStore.DB.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM alert_events WHERE id = 'event'").Scan(&count); err != nil {
		t.Fatalf("count application rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("application row count = %d, want 1", count)
	}
	usage, err := sqliteStore.DiskUsage()
	if err != nil || usage <= 0 {
		t.Fatalf("SQLite usage = %d, err = %v", usage, err)
	}
}

func TestMTSMaintenanceDeletesOnlyExpiredSamples(t *testing.T) {
	mtsStore, err := NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	oldTime := now.Add(-48 * time.Hour)
	newTime := now.Add(-time.Hour)
	for _, timestamp := range []time.Time{oldTime, newTime} {
		if err := mtsStore.WriteMetric(t.Context(), "node", "cpu", 1, timestamp); err != nil {
			t.Fatalf("write metric: %v", err)
		}
	}
	if err := mtsStore.CleanupBefore(t.Context(), now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("cleanup MTS: %v", err)
	}
	points, err := mtsStore.QueryMetrics(t.Context(), []string{"cpu"}, oldTime.Add(-time.Hour), now, "node")
	if err != nil || len(points["cpu"]) != 1 || !points["cpu"][0].Timestamp.Equal(newTime) {
		t.Fatalf("remaining points = %+v, err = %v", points, err)
	}
	usage, err := mtsStore.DiskUsage()
	if err != nil || usage <= 0 {
		t.Fatalf("MTS usage = %d, err = %v", usage, err)
	}
	healthy, _ := mtsStore.Health()
	if !healthy {
		t.Fatal("MTS health = false")
	}
}

func TestMaintenancePathAndCutoffValidation(t *testing.T) {
	if sqliteFilesystemPath(":memory:") != "" ||
		sqliteFilesystemPath("file::memory:?cache=shared") != "" ||
		sqliteFilesystemPath("file:test.db?cache=shared") != "test.db" {
		t.Fatal("SQLite filesystem path normalization failed")
	}
	mtsStore, err := NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	if err := mtsStore.CleanupBefore(t.Context(), time.Time{}); err == nil {
		t.Fatal("zero cutoff error = nil")
	}
}

func TestSQLiteMaintenanceClosedDatabaseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed-maintenance.db")
	sqliteStore, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close SQLite store: %v", err)
	}
	if _, err := sqliteStore.Maintain(t.Context()); err == nil {
		t.Fatal("maintenance on closed database error = nil")
	}
	if err := sqliteStore.checkpoint(t.Context()); err == nil {
		t.Fatal("checkpoint on closed database error = nil")
	}
	if _, err := sqliteStore.integrityCheck(t.Context()); err == nil {
		t.Fatal("integrity check on closed database error = nil")
	}
}

func TestSQLiteDiskUsageMemoryDatabase(t *testing.T) {
	sqliteStore, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create in-memory SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	usage, err := sqliteStore.DiskUsage()
	if err != nil || usage != 0 {
		t.Fatalf("memory database usage = %d, err = %v", usage, err)
	}
}
