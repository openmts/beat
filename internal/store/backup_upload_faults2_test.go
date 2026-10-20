package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func covUploadActor() BackupUploadActor {
	return BackupUploadActor{UserID: "owner-1", Username: "owner", RequestID: "request"}
}

func covOpenUploadSession(t *testing.T, store *BackupUploadStore, id string,
	now time.Time) model.BackupUploadSession {
	t.Helper()
	session := newUploadSession(id, now)
	if err := store.Create(t.Context(), covUploadActor(), &session); err != nil {
		t.Fatal(err)
	}
	return session
}

// TestBackupUploadStoreCreateFaults 覆盖建立会话的事务、上限计数、插入、审计与提交失败。
func TestBackupUploadStoreCreateFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("closed database", func(t *testing.T) {
		store := NewBackupUploadStore(closedSQLiteStore(t).DB)
		session := newUploadSession("session-closed", now)
		if err := store.Create(t.Context(), covUploadActor(), &session); err == nil {
			t.Fatal("closed database created a session")
		}
	})
	t.Run("count failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		if _, err := database.DB.Exec("ALTER TABLE backup_upload_sessions RENAME TO cov_upload_backup"); err != nil {
			t.Fatal(err)
		}
		session := newUploadSession("session-count", now)
		if err := store.Create(t.Context(), covUploadActor(), &session); err == nil {
			t.Fatal("unreadable session ledger was accepted")
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_insert
			BEFORE INSERT ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		session := newUploadSession("session-insert", now)
		if err := store.Create(t.Context(), covUploadActor(), &session); err == nil {
			t.Fatal("blocked session insert was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		session := newUploadSession("session-audit", now)
		if err := store.Create(t.Context(), covUploadActor(), &session); err == nil {
			t.Fatal("blocked session audit was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		installCustomMetricCommitFault(t, database.DB, "INSERT ON backup_upload_sessions")
		session := newUploadSession("session-commit", now)
		if err := store.Create(t.Context(), covUploadActor(), &session); err == nil {
			t.Fatal("deferred commit failure was accepted")
		}
	})
}

// TestBackupUploadStoreTransitionFaults 覆盖状态迁移与校验绑定的五类存储故障。
func TestBackupUploadStoreTransitionFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("closed database", func(t *testing.T) {
		store := NewBackupUploadStore(closedSQLiteStore(t).DB)
		if err := store.SetState(t.Context(), covUploadActor(), "session-1",
			model.BackupUploadStateCancelled, "cancelled", now); err == nil {
			t.Fatal("closed database transitioned a session")
		}
		if err := store.MarkValidated(t.Context(), covUploadActor(), "session-1", "backup-1", now); err == nil {
			t.Fatal("closed database validated a session")
		}
	})
	t.Run("update blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-update", now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_update
			BEFORE UPDATE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'update blocked'); END`)
		if err := store.SetState(t.Context(), covUploadActor(), session.ID,
			model.BackupUploadStateCancelled, "cancelled", now); err == nil {
			t.Fatal("blocked transition update was accepted")
		}
		if err := store.MarkValidated(t.Context(), covUploadActor(), session.ID, "backup-1", now); err == nil {
			t.Fatal("blocked validation update was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-audit", now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_transition_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if err := store.SetState(t.Context(), covUploadActor(), session.ID,
			model.BackupUploadStateCancelled, "cancelled", now); err == nil {
			t.Fatal("blocked transition audit was accepted")
		}
		if err := store.MarkValidated(t.Context(), covUploadActor(), session.ID, "backup-1", now); err == nil {
			t.Fatal("blocked validation audit was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-commit", now)
		installCustomMetricCommitFault(t, database.DB, "UPDATE ON backup_upload_sessions")
		if err := store.SetState(t.Context(), covUploadActor(), session.ID,
			model.BackupUploadStateCancelled, "cancelled", now); err == nil {
			t.Fatal("deferred transition commit failure was accepted")
		}
		if err := store.MarkValidated(t.Context(), covUploadActor(), session.ID, "backup-1", now); err == nil {
			t.Fatal("deferred validation commit failure was accepted")
		}
	})
}

// TestBackupUploadStoreScanFaults 覆盖会话列表读取失败：非可空列被写成 NULL 时失败关闭。
func TestBackupUploadStoreScanFaults(t *testing.T) {
	database := setupTestDB(t)
	store := NewBackupUploadStore(database.DB)
	now := time.Now().UTC()
	session := covOpenUploadSession(t, store, "session-quarantined", now)
	if err := store.SetState(t.Context(), covUploadActor(), session.ID,
		model.BackupUploadStateQuarantined, "rejected", now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("ALTER TABLE backup_upload_sessions RENAME TO cov_upload_backup"); err != nil {
		t.Fatal(err)
	}
	if err := installUploadSessionView(database); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListByOwner(t.Context(), session.OwnerUserID, 10); err == nil {
		t.Fatal("unscannable owner list was accepted")
	}
	if _, err := store.ListQuarantined(t.Context(), 10); err == nil {
		t.Fatal("unscannable quarantine list was accepted")
	}
}

func installUploadSessionView(database *SQLiteStore) error {
	_, err := database.DB.Exec(`CREATE VIEW backup_upload_sessions AS SELECT id, owner_user_id,
		NULL AS filename, declared_bytes, declared_digest, chunk_bytes, chunk_count, received_bytes,
		received_chunks, staging_name, state, backup_id, last_error, staging_removed, expires_at,
		created_at, updated_at FROM cov_upload_backup`)
	return err
}

// TestBackupUploadStoreQuarantineListing 覆盖被拒会话清单的正常读取与非法分页。
func TestBackupUploadStoreQuarantineListing(t *testing.T) {
	database := setupTestDB(t)
	store := NewBackupUploadStore(database.DB)
	now := time.Now().UTC()
	quarantined := covOpenUploadSession(t, store, "session-rejected", now.Add(time.Second))
	actor := covUploadActor()
	chunk := covUploadChunk(quarantined.ID, 0, now, strings.Repeat("b", 64))
	if _, _, err := store.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: actor, Session: quarantined, Chunk: chunk, Now: now}); err != nil {
		t.Fatal(err)
	}
	conflict := chunk
	conflict.Digest = strings.Repeat("c", 64)
	if _, _, err := store.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: actor, Session: quarantined, Chunk: conflict, Now: now}); !errors.Is(err, ErrBackupUploadChunkConflict) {
		t.Fatalf("conflicting chunk = %v", err)
	}
	list, err := store.ListQuarantined(t.Context(), 10)
	if err != nil || len(list) != 1 || list[0].ID != quarantined.ID {
		t.Fatalf("quarantine list = %+v, %v", list, err)
	}
	if _, err := store.ListQuarantined(t.Context(), 0); !errors.Is(err, ErrInvalidBackupQuery) {
		t.Fatalf("invalid page = %v", err)
	}
	chunks, err := store.ListChunks(t.Context(), quarantined.ID)
	if err != nil || len(chunks) != 1 || chunks[0].Index != 0 {
		t.Fatalf("chunk list = %+v, %v", chunks, err)
	}
	stored, err := store.Chunk(t.Context(), quarantined.ID, 0)
	if err != nil || stored == nil || stored.Digest != chunk.Digest {
		t.Fatalf("stored chunk = %+v, %v", stored, err)
	}
}
