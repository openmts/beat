package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func seedMaintenanceUploadSession(t *testing.T, database *sql.DB, id, staging, state, lastError string,
	removed int, expires, now time.Time) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), `INSERT INTO backup_upload_sessions(id, owner_user_id,
		filename, declared_bytes, declared_digest, chunk_bytes, chunk_count, staging_name, state,
		last_error, staging_removed, expires_at, created_at, updated_at)
		VALUES(?, 'owner-maintenance', 'archive.zip', 0, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		id, strings.Repeat("a", 64), model.MinimumBackupUploadChunkBytes, staging, state, lastError,
		removed, expires, now, now); err != nil {
		t.Fatal(err)
	}
}

func maintenancedExpiredSession(t *testing.T, store *BackupUploadStore, now time.Time) {
	t.Helper()
	session := newUploadSession("session-expired", now)
	session.ExpiresAt = now.Add(-time.Minute)
	if err := store.Create(t.Context(), covUploadActor(), &session); err != nil {
		t.Fatal(err)
	}
}

func maintenancedPrunableSession(t *testing.T, store *BackupUploadStore, now time.Time) {
	t.Helper()
	session := covOpenUploadSession(t, store, "session-prunable", now)
	if err := appendCovUploadChunk(t, store, session,
		covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64)), now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStagingRemoved(t.Context(), session.ID, now); err != nil {
		t.Fatal(err)
	}
}

// TestBackupUploadExpireFaults 覆盖过期事务的选择、扫描、更新、审计与提交失败。
func TestBackupUploadExpireFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("select blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricFault(t, database.DB,
			"ALTER TABLE backup_upload_sessions RENAME TO cov_upload_maintenance_backup")
		if _, err := store.Expire(t.Context(), now); err == nil {
			t.Fatal("unreadable session ledger accepted an expiry sweep")
		}
	})
	t.Run("scan failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		seedMaintenanceUploadSession(t, database.DB, "null", "u-null-open", "open", "", 0, now.Add(-time.Minute), now)
		// 文本主键在 SQLite 里允许 NULL，扫描到非可空字段时失败关闭。
		if _, err := database.DB.ExecContext(t.Context(), "UPDATE backup_upload_sessions SET id = NULL WHERE staging_name = 'u-null-open'"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Expire(t.Context(), now); err == nil {
			t.Fatal("NULL session id was accepted")
		}
	})
	t.Run("update blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedExpiredSession(t, store, now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_expire_update
			BEFORE UPDATE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'expire blocked'); END`)
		if _, err := store.Expire(t.Context(), now); err == nil {
			t.Fatal("blocked expiry update was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedExpiredSession(t, store, now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_expire_audit
			BEFORE INSERT ON admin_audit_events WHEN NEW.action = 'backup.upload.expired'
			BEGIN SELECT RAISE(ABORT,'expiry audit blocked'); END`)
		if _, err := store.Expire(t.Context(), now); err == nil {
			t.Fatal("blocked expiry audit was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedExpiredSession(t, store, now)
		installCustomMetricCommitFault(t, database.DB, "UPDATE ON backup_upload_sessions")
		if _, err := store.Expire(t.Context(), now); err == nil {
			t.Fatal("deferred expiry commit failure was accepted")
		}
	})
}

// TestBackupUploadStagingCleanupFaults 覆盖暂存清理清单的扫描、游标迭代与标记失败。
func TestBackupUploadStagingCleanupFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("scan failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		seedMaintenanceUploadSession(t, database.DB, "null", "u-null-validated", "validated", "", 0, now, now)
		if _, err := database.DB.ExecContext(t.Context(), "UPDATE backup_upload_sessions SET id = NULL WHERE staging_name = 'u-null-validated'"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PendingStagingCleanup(t.Context(), 10); err == nil {
			t.Fatal("NULL session id reached the cleanup list")
		}
	})
	t.Run("iteration failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		seedMaintenanceUploadSession(t, database.DB, "session-a", "u-a", "validated", "", 0, now, now)
		seedMaintenanceUploadSession(t, database.DB, "session-b", "u-b", "validated", "boom", 0, now, now)
		installCustomMetricFault(t, database.DB,
			"ALTER TABLE backup_upload_sessions RENAME TO cov_upload_maintenance_backup",
			`CREATE VIEW backup_upload_sessions AS SELECT id, owner_user_id, filename, declared_bytes,
			 declared_digest, chunk_bytes, chunk_count, received_bytes, received_chunks, staging_name,
			 state, backup_id, CASE WHEN last_error = 'boom' THEN abs(-9223372036854775808)
			 ELSE last_error END AS last_error, staging_removed, expires_at, created_at, updated_at
			 FROM cov_upload_maintenance_backup`)
		if _, err := store.PendingStagingCleanup(t.Context(), 10); err == nil {
			t.Fatal("expired cleanup cursor was accepted")
		}
	})
	t.Run("mark removed blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricFault(t, database.DB,
			"ALTER TABLE backup_upload_sessions RENAME TO cov_upload_maintenance_backup")
		if err := store.MarkStagingRemoved(t.Context(), "session-a", now); err == nil {
			t.Fatal("unreadable session ledger accepted a staging mark")
		}
	})
}

// TestBackupUploadPruneFaults 覆盖修剪事务的开启、选择、扫描、删除与提交失败。
func TestBackupUploadPruneFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("closed database", func(t *testing.T) {
		store := NewBackupUploadStore(closedSQLiteStore(t).DB)
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("closed database accepted a prune")
		}
	})
	t.Run("select blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricFault(t, database.DB,
			"ALTER TABLE backup_upload_sessions RENAME TO cov_upload_maintenance_backup")
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("unreadable session ledger accepted a prune")
		}
	})
	t.Run("scan failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		seedMaintenanceUploadSession(t, database.DB, "null", "u-null-removed", "validated", "", 1, now, now)
		if _, err := database.DB.ExecContext(t.Context(), "UPDATE backup_upload_sessions SET id = NULL WHERE staging_name = 'u-null-removed'"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("NULL session id reached the prune list")
		}
	})
	t.Run("chunk delete blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedPrunableSession(t, store, now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_prune_chunks
			BEFORE DELETE ON backup_upload_chunks BEGIN SELECT RAISE(ABORT,'chunk delete blocked'); END`)
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("blocked chunk delete was accepted")
		}
	})
	t.Run("session delete blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedPrunableSession(t, store, now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_prune_sessions
			BEFORE DELETE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'session delete blocked'); END`)
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("blocked session delete was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		maintenancedPrunableSession(t, store, now)
		installCustomMetricCommitFault(t, database.DB, "DELETE ON backup_upload_sessions")
		if _, err := store.Prune(t.Context(), 0, 10); err == nil {
			t.Fatal("deferred prune commit failure was accepted")
		}
	})
}
