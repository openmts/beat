package store

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const covUploadDeclaredBytes = 2 * model.MinimumBackupUploadChunkBytes

func covUploadChunk(sessionID string, index int, now time.Time, digest string) model.BackupUploadChunk {
	return model.BackupUploadChunk{SessionID: sessionID, Index: index,
		Offset: model.OffsetForBackupUploadChunk(index, model.MinimumBackupUploadChunkBytes),
		SizeBytes: model.BackupUploadChunkSize(index, model.MinimumBackupUploadChunkBytes,
			covUploadDeclaredBytes),
		Digest: digest, CreatedAt: now}
}

func appendCovUploadChunk(t *testing.T, store *BackupUploadStore, session model.BackupUploadSession,
	chunk model.BackupUploadChunk, now time.Time) error {
	t.Helper()
	_, _, err := store.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: covUploadActor(), Session: session, Chunk: chunk, Now: now})
	return err
}

// TestBackupUploadChunkSessionFaults 覆盖分块提交前的会话读取、状态与过期失败。
func TestBackupUploadChunkSessionFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("closed database", func(t *testing.T) {
		store := NewBackupUploadStore(closedSQLiteStore(t).DB)
		chunk := covUploadChunk("session-1", 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, model.BackupUploadSession{}, chunk, now); err == nil {
			t.Fatal("closed database appended a chunk")
		}
	})
	t.Run("session read failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		if _, err := database.DB.Exec(
			"ALTER TABLE backup_upload_sessions RENAME TO cov_upload_backup"); err != nil {
			t.Fatal(err)
		}
		chunk := covUploadChunk("session-1", 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, model.BackupUploadSession{}, chunk, now); err == nil {
			t.Fatal("unreadable session was accepted")
		}
	})
	t.Run("session not open", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-validated", now)
		if err := store.MarkValidated(t.Context(), covUploadActor(), session.ID, "backup-1", now); err != nil {
			t.Fatal(err)
		}
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err != ErrBackupUploadClosed {
			t.Fatalf("non-open session = %v", err)
		}
	})
	t.Run("expire blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := newUploadSession("session-expired", now)
		session.ExpiresAt = now.Add(-time.Minute)
		if err := store.Create(t.Context(), covUploadActor(), &session); err != nil {
			t.Fatal(err)
		}
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_expire
			BEFORE UPDATE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'expire blocked'); END`)
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("blocked expiry was accepted")
		}
	})
	t.Run("expired commit failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := newUploadSession("session-expired-commit", now)
		session.ExpiresAt = now.Add(-time.Minute)
		if err := store.Create(t.Context(), covUploadActor(), &session); err != nil {
			t.Fatal(err)
		}
		installCustomMetricCommitFault(t, database.DB, "UPDATE ON backup_upload_sessions")
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("deferred expiry commit failure was accepted")
		}
	})
}

// TestBackupUploadChunkLedgerFaults 覆盖分块账本读取、冲突隔离、插入、进度推进与提交失败。
func TestBackupUploadChunkLedgerFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("chunk read failure", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-chunk-read", now)
		if _, err := database.DB.Exec("ALTER TABLE backup_upload_chunks RENAME TO cov_chunks_backup"); err != nil {
			t.Fatal(err)
		}
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("unreadable chunk ledger was accepted")
		}
	})
	t.Run("quarantine blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-quarantine", now)
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err != nil {
			t.Fatal(err)
		}
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_quarantine
			BEFORE UPDATE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'quarantine blocked'); END`)
		conflict := chunk
		conflict.Digest = strings.Repeat("c", 64)
		if err := appendCovUploadChunk(t, store, session, conflict, now); err == nil {
			t.Fatal("blocked quarantine was accepted")
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-chunk-insert", now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_chunk_insert
			BEFORE INSERT ON backup_upload_chunks BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("blocked chunk insert was accepted")
		}
	})
	t.Run("advance blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-chunk-advance", now)
		installCustomMetricFault(t, database.DB, `CREATE TRIGGER cov_upload_advance
			BEFORE UPDATE ON backup_upload_sessions BEGIN SELECT RAISE(ABORT,'advance blocked'); END`)
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("blocked progress advance was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewBackupUploadStore(database.DB)
		session := covOpenUploadSession(t, store, "session-chunk-commit", now)
		installCustomMetricCommitFault(t, database.DB, "INSERT ON backup_upload_chunks")
		chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
		if err := appendCovUploadChunk(t, store, session, chunk, now); err == nil {
			t.Fatal("deferred chunk commit failure was accepted")
		}
	})
}

// TestBackupUploadListChunksScanFaults 覆盖分块列表读取失败。
func TestBackupUploadListChunksScanFaults(t *testing.T) {
	database := setupTestDB(t)
	store := NewBackupUploadStore(database.DB)
	now := time.Now().UTC()
	session := covOpenUploadSession(t, store, "session-chunk-scan", now)
	chunk := covUploadChunk(session.ID, 0, now, strings.Repeat("b", 64))
	if err := appendCovUploadChunk(t, store, session, chunk, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("ALTER TABLE backup_upload_chunks RENAME TO cov_chunks_backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE VIEW backup_upload_chunks AS SELECT session_id,
		chunk_index, offset_bytes, size_bytes, NULL AS digest, created_at FROM cov_chunks_backup`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListChunks(t.Context(), session.ID); err == nil {
		t.Fatal("unscannable chunk list was accepted")
	}
}
