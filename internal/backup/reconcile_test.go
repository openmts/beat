package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// readyFixtureBackup 生成一个带完整归档的 ready 记录，作为对账矩阵的基线。
func readyFixtureBackup(t *testing.T, fixture *backupFixture) model.BackupRecord {
	t.Helper()
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	loaded, err := fixture.service.record(t.Context(), record.ID)
	if err != nil || loaded == nil || loaded.State != model.BackupStateReady {
		t.Fatalf("fixture backup = %#v, %v", loaded, err)
	}
	return *loaded
}

func updateBackupRow(t *testing.T, fixture *backupFixture, id string, columns map[string]any) {
	t.Helper()
	for column, value := range columns {
		if _, err := fixture.sqlite.DB.Exec(
			"UPDATE admin_backups SET "+column+" = ? WHERE id = ?", value, id); err != nil {
			t.Fatalf("update backup %s: %v", column, err)
		}
	}
}

func loadBackupRow(t *testing.T, fixture *backupFixture, id string) *model.BackupRecord {
	t.Helper()
	record, err := store.NewBackupStore(fixture.sqlite.DB).Get(t.Context(), id)
	if err != nil {
		t.Fatalf("load backup %s: %v", id, err)
	}
	return record
}

func TestReconcileInterruptsRunningBackupWithoutArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	if err := os.Remove(filepath.Join(fixture.dataDir, "backups", record.Filename)); err != nil {
		t.Fatalf("remove archive: %v", err)
	}
	updateBackupRow(t, fixture, record.ID, map[string]any{"state": model.BackupStateRunning,
		"completed_at": nil})
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Interrupted != 1 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	loaded := loadBackupRow(t, fixture, record.ID)
	if loaded == nil || loaded.State != model.BackupStateInterrupted || loaded.ErrorMessage == "" {
		t.Fatalf("interrupted record = %#v", loaded)
	}
	if !hasAuditAction(t, fixture, "backup.reconcile.interrupted") {
		t.Fatal("interrupted transition was not audited")
	}
}

func TestReconcileRecoversRunningBackupWithCompleteArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	updateBackupRow(t, fixture, record.ID, map[string]any{"state": model.BackupStateRunning,
		"completed_at": nil})
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Recovered != 1 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	loaded := loadBackupRow(t, fixture, record.ID)
	if loaded == nil || loaded.State != model.BackupStateReady || loaded.CompletedAt == nil {
		t.Fatalf("recovered record = %#v", loaded)
	}
}

func TestReconcileMarksMissingArchivesWithoutFollowingLinks(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	archive := filepath.Join(fixture.dataDir, "backups", record.Filename)
	if err := os.Remove(archive); err != nil {
		t.Fatalf("remove archive: %v", err)
	}
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Missing != 1 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	if loaded := loadBackupRow(t, fixture, record.ID); loaded == nil ||
		loaded.State != model.BackupStateMissing {
		t.Fatalf("missing record = %#v", loaded)
	}

	// 符号链接与目录都不能被当作可用归档，也绝不能被跟随或删除。
	target := filepath.Join(t.TempDir(), "target.zip")
	if err := os.WriteFile(target, []byte("archive"), 0o600); err != nil {
		t.Fatalf("write link target: %v", err)
	}
	if err := os.Symlink(target, archive); err != nil {
		t.Fatalf("create symlink archive: %v", err)
	}
	if _, err := fixture.service.Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile symlink: %v", err)
	}
	if info, err := os.Lstat(archive); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink archive was modified: %#v, %v", info, err)
	}
	if err := os.Remove(archive); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.Mkdir(archive, 0o700); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	outcome, err = fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Missing != 0 {
		t.Fatalf("directory outcome = %#v, %v", outcome, err)
	}
	if loaded := loadBackupRow(t, fixture, record.ID); loaded == nil ||
		loaded.State != model.BackupStateMissing {
		t.Fatalf("directory record = %#v", loaded)
	}
	if info, err := os.Lstat(archive); err != nil || !info.IsDir() {
		t.Fatalf("archive directory was modified: %#v, %v", info, err)
	}
}

func TestReconcileCompletesAndDefersDeletions(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	updateBackupRow(t, fixture, record.ID, map[string]any{"state": model.BackupStateDeleting})
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Deleted != 1 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	if loadBackupRow(t, fixture, record.ID) != nil {
		t.Fatal("deleting record survived reconciliation")
	}
	if _, err := os.Lstat(filepath.Join(fixture.dataDir, "backups", record.Filename)); !os.IsNotExist(err) {
		t.Fatalf("archive survived deletion: %v", err)
	}

	// 归档缺失时删除必须保持幂等。
	missing := readyFixtureBackup(t, fixture)
	if err := os.Remove(filepath.Join(fixture.dataDir, "backups", missing.Filename)); err != nil {
		t.Fatalf("remove archive: %v", err)
	}
	updateBackupRow(t, fixture, missing.ID, map[string]any{"state": model.BackupStateDeleting})
	if _, err := fixture.service.Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile missing deletion: %v", err)
	}
	if loadBackupRow(t, fixture, missing.ID) != nil {
		t.Fatal("deleting record without archive survived reconciliation")
	}

	// 无法安全删除的条目必须保留可重试状态，而不是让启动失败。
	blocked := readyFixtureBackup(t, fixture)
	archive := filepath.Join(fixture.dataDir, "backups", blocked.Filename)
	if err := os.Remove(archive); err != nil {
		t.Fatalf("remove archive: %v", err)
	}
	if err := os.Mkdir(archive, 0o700); err != nil {
		t.Fatalf("create obstruction: %v", err)
	}
	updateBackupRow(t, fixture, blocked.ID, map[string]any{"state": model.BackupStateDeleting})
	outcome, err = fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Deferred != 1 {
		t.Fatalf("blocked outcome = %#v, %v", outcome, err)
	}
	if loaded := loadBackupRow(t, fixture, blocked.ID); loaded == nil ||
		loaded.State != model.BackupStateDeleting {
		t.Fatalf("blocked record = %#v", loaded)
	}
}

func hasAuditAction(t *testing.T, fixture *backupFixture, action string) bool {
	t.Helper()
	var count int
	if err := fixture.sqlite.DB.QueryRow(
		"SELECT COUNT(*) FROM admin_audit_events WHERE action = ?", action).Scan(&count); err != nil {
		t.Fatalf("count audit action %s: %v", action, err)
	}
	return count > 0
}
