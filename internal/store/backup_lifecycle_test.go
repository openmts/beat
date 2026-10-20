package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func backupTransitionAudit(id, action string) model.AdminAuditEvent {
	return model.AdminAuditEvent{ID: "audit-" + id, ActorUsername: "system",
		ActorType: model.AuditActorService, Action: action, ResourceType: "backup",
		ResourceID: id, Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}",
		CreatedAt: time.Now().UTC()}
}

func TestBackupTransitionRequiresExpectedStateAndAudits(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := time.Now().UTC()
	record := model.BackupRecord{ID: "backup", Filename: "backup.zip", Source: "generated",
		State: model.BackupStateReady, CreatedAt: now}
	if err := store.Create(t.Context(), &record); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	deleting := record
	deleting.State = model.BackupStateDeleting
	deleting.PreviousState = model.BackupStateReady
	deleting.UpdatedAt = now
	audit := backupTransitionAudit("backup", "backup.delete.started")
	if err := store.Transition(t.Context(), BackupTransition{Record: deleting,
		Expected: []string{model.BackupStateReady}, Audit: audit}); err != nil {
		t.Fatalf("transition backup: %v", err)
	}
	loaded, err := store.Get(t.Context(), "backup")
	if err != nil || loaded == nil || loaded.State != model.BackupStateDeleting ||
		loaded.PreviousState != model.BackupStateReady {
		t.Fatalf("transitioned record = %#v, %v", loaded, err)
	}
	var audits int
	if err := sqliteStore.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE action = 'backup.delete.started'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("transition audit rows = %d, %v", audits, err)
	}
	// 状态已被推进：同样的 CAS 必须冲突，且不得再写审计。
	if err := store.Transition(t.Context(), BackupTransition{Record: deleting,
		Expected: []string{model.BackupStateReady}, Audit: audit}); !errors.Is(err, ErrBackupStateConflict) {
		t.Fatalf("stale transition = %v", err)
	}
	if err := sqliteStore.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE action = 'backup.delete.started'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("conflicting transition wrote audit rows = %d, %v", audits, err)
	}
	if err := store.Transition(t.Context(), BackupTransition{Record: deleting}); err == nil {
		t.Fatal("transition without expected states succeeded")
	}
}

func TestBackupTransitionRollsBackAuditFailure(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := time.Now().UTC()
	record := model.BackupRecord{ID: "backup", Filename: "backup.zip", Source: "generated",
		State: model.BackupStateReady, CreatedAt: now}
	if err := store.Create(t.Context(), &record); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	next := record
	next.State = model.BackupStateMissing
	next.UpdatedAt = now
	// 审计主键冲突必须让整个状态推进回滚，而不是留下无审计的状态变化。
	audit := backupTransitionAudit("backup", "backup.reconcile.missing")
	seed := audit
	seed.Action = "seed"
	if err := NewAdminStore(sqliteStore.DB).CreateAuditEvent(t.Context(), &seed); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
	if err := store.Transition(t.Context(), BackupTransition{Record: next,
		Expected: []string{model.BackupStateReady}, Audit: audit}); err == nil {
		t.Fatal("transition with duplicate audit id succeeded")
	}
	loaded, err := store.Get(t.Context(), "backup")
	if err != nil || loaded == nil || loaded.State != model.BackupStateReady {
		t.Fatalf("failed transition changed state: %#v, %v", loaded, err)
	}
}

func TestExpireTerminalBackupsKeepsRecentAndLiveRecords(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := time.Now().UTC()
	seed := []model.BackupRecord{
		{ID: "old-failed", Filename: "beat-old-failed.zip", Source: "generated",
			State: model.BackupStateFailed, CreatedAt: now.Add(-120 * 24 * time.Hour)},
		{ID: "old-missing", Filename: "beat-old-missing.zip", Source: "generated",
			State: model.BackupStateMissing, CreatedAt: now.Add(-100 * 24 * time.Hour)},
		{ID: "recent-failed", Filename: "beat-recent-failed.zip", Source: "generated",
			State: model.BackupStateFailed, CreatedAt: now.Add(-time.Hour)},
		{ID: "old-ready", Filename: "beat-old-ready.zip", Source: "generated",
			State: model.BackupStateReady, CreatedAt: now.Add(-400 * 24 * time.Hour)},
	}
	for index := range seed {
		if err := store.Create(t.Context(), &seed[index]); err != nil {
			t.Fatalf("create %s: %v", seed[index].ID, err)
		}
	}
	expired, err := store.ExpireTerminal(t.Context(), model.BackupTerminalStates,
		now.Add(-90*24*time.Hour))
	if err != nil || expired != 2 {
		t.Fatalf("expired rows = %d, %v", expired, err)
	}
	page, err := store.ListBackupsPage(t.Context(), BackupQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("remaining backups = %#v, %v", page.Items, err)
	}
	if expired, err := store.ExpireTerminal(t.Context(), nil, now); err != nil || expired != 0 {
		t.Fatalf("empty state list = %d, %v", expired, err)
	}
}

func TestBackupQuarantineInventoryRecordsOnce(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := time.Now().UTC()
	record := model.BackupQuarantineRecord{ID: "quarantine", Filename: "beat-orphan.zip",
		SizeBytes: 42, Reason: "orphan", QuarantinedAt: now}
	if err := store.RecordQuarantine(t.Context(), &record); err != nil {
		t.Fatalf("record quarantine: %v", err)
	}
	record.ID = "duplicate"
	if err := store.RecordQuarantine(t.Context(), &record); err != nil {
		t.Fatalf("duplicate quarantine: %v", err)
	}
	count, err := store.CountQuarantine(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("quarantine count = %d, %v", count, err)
	}
}

const legacyAdminBackupsDDL = `CREATE TABLE admin_backups (
	id TEXT PRIMARY KEY,
	filename TEXT NOT NULL UNIQUE,
	source TEXT NOT NULL CHECK(source IN ('generated', 'uploaded')),
	state TEXT NOT NULL CHECK(state IN ('running', 'ready', 'failed', 'validated', 'staged')),
	created_at DATETIME NOT NULL,
	completed_at DATETIME,
	snapshot_cutoff DATETIME,
	size_bytes INTEGER NOT NULL DEFAULT 0,
	sqlite_bytes INTEGER NOT NULL DEFAULT 0,
	metrics_bytes INTEGER NOT NULL DEFAULT 0,
	metric_rows INTEGER NOT NULL DEFAULT 0,
	error_message TEXT NOT NULL DEFAULT ''
)`

// TestMigrateBackupLifecycleRebuildsLegacyTable 证明旧表被事务内重建且数据完整保留。
func TestMigrateBackupLifecycleRebuildsLegacyTable(t *testing.T) {
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	defer func() { _ = database.Close() }()
	legacyStatements := []string{"DROP TABLE admin_backups", legacyAdminBackupsDDL,
		`INSERT INTO admin_backups (id, filename, source, state, created_at, size_bytes,
			metric_rows, error_message) VALUES ('legacy', 'beat-legacy.zip', 'generated',
			'ready', '2026-01-02 03:04:05', 128, 9, '')`}
	for _, statement := range legacyStatements {
		if _, err := database.DB.Exec(statement); err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateBackupLifecycleFixture(t, database); err != nil {
			t.Fatalf("migrate attempt %d: %v", attempt, err)
		}
	}
	store := NewBackupStore(database.DB)
	record, err := store.Get(t.Context(), "legacy")
	if err != nil || record == nil || record.SizeBytes != 128 || record.MetricRows != 9 ||
		record.State != model.BackupStateReady || !record.UpdatedAt.Equal(record.CreatedAt) {
		t.Fatalf("legacy record = %#v, %v", record, err)
	}
	// 重建后的约束必须接受新的生命周期状态。
	record.State = model.BackupStateDeleting
	record.UpdatedAt = time.Now().UTC()
	if err := store.Update(t.Context(), record); err != nil {
		t.Fatalf("update rebuilt record: %v", err)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'admin_backup_quarantine'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("quarantine table missing: %v", err)
	}
}

func migrateBackupLifecycleFixture(t *testing.T, database *SQLiteStore) error {
	t.Helper()
	tx, err := database.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := MigrateBackupLifecycle(t.Context(), tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestListQuarantineIsBoundedAndOrdered(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewBackupStore(sqliteStore.DB)
	now := time.Now().UTC()
	for index := 0; index < 3; index++ {
		record := model.BackupQuarantineRecord{ID: fmt.Sprintf("quarantine-%d", index),
			Filename: fmt.Sprintf("beat-orphan-%d.zip", index), SizeBytes: int64(index),
			Reason: "orphan", QuarantinedAt: now.Add(time.Duration(index) * time.Minute)}
		if err := store.RecordQuarantine(t.Context(), &record); err != nil {
			t.Fatalf("record quarantine %d: %v", index, err)
		}
	}
	records, err := store.ListQuarantine(t.Context(), 2)
	if err != nil || len(records) != 2 || records[0].ID != "quarantine-2" {
		t.Fatalf("quarantine page = %#v, %v", records, err)
	}
	for _, limit := range []int{0, -1, BackupMaximumPage + 1} {
		if _, err := store.ListQuarantine(t.Context(), limit); err != ErrInvalidBackupQuery {
			t.Fatalf("limit %d returned %v", limit, err)
		}
	}
}
