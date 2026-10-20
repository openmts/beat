package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrBackupUploadChunkConflict 表示同一索引收到了不同字节。
var ErrBackupUploadChunkConflict = errors.New("backup upload chunk conflicts with the recorded bytes")

// BackupUploadChunkAppend 描述一次分块提交。
type BackupUploadChunkAppend struct {
	Actor   BackupUploadActor
	Session model.BackupUploadSession
	Chunk   model.BackupUploadChunk
	Now     time.Time
}

// ListChunks 按索引升序返回已提交分块。
func (store *BackupUploadStore) ListChunks(
	ctx context.Context,
	sessionID string,
) ([]model.BackupUploadChunk, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT session_id, chunk_index, offset_bytes,
		size_bytes, digest, created_at FROM backup_upload_chunks WHERE session_id = ?
		ORDER BY chunk_index`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list backup upload chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	chunks := []model.BackupUploadChunk{}
	for rows.Next() {
		chunk, scanErr := scanBackupUploadChunk(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan backup upload chunk: %w", scanErr)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup upload chunks: %w", err)
	}
	return chunks, nil
}

// Chunk 返回指定索引的已提交分块；不存在时返回 (nil, nil)。
func (store *BackupUploadStore) Chunk(
	ctx context.Context,
	sessionID string,
	index int,
) (*model.BackupUploadChunk, error) {
	row := store.db.QueryRowContext(ctx, `SELECT session_id, chunk_index, offset_bytes,
		size_bytes, digest, created_at FROM backup_upload_chunks
		WHERE session_id = ? AND chunk_index = ?`, sessionID, index)
	chunk, err := scanBackupUploadChunk(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get backup upload chunk: %w", err)
	}
	return &chunk, nil
}

// AppendChunk 幂等地提交一个分块。
//
// 返回的 second 值表示该索引此前已存在且字节摘要一致；冲突摘要会让会话进入
// quarantined 并返回 ErrBackupUploadChunkConflict。
func (store *BackupUploadStore) AppendChunk(
	ctx context.Context,
	input BackupUploadChunkAppend,
) (model.BackupUploadChunk, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BackupUploadChunk{}, false, fmt.Errorf("begin backup upload chunk: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	chunk := input.Chunk
	var state string
	var expiresAt time.Time
	var chunkCount int
	var chunkBytes, declaredBytes int64
	err = tx.QueryRowContext(ctx, `SELECT state, expires_at, chunk_count, chunk_bytes,
		declared_bytes FROM backup_upload_sessions WHERE id = ?`, chunk.SessionID).
		Scan(&state, &expiresAt, &chunkCount, &chunkBytes, &declaredBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BackupUploadChunk{}, false, ErrBackupUploadClosed
	}
	if err != nil {
		return model.BackupUploadChunk{}, false, fmt.Errorf("read backup upload session: %w", err)
	}
	if state != model.BackupUploadStateOpen {
		return model.BackupUploadChunk{}, false, ErrBackupUploadClosed
	}
	geometry := model.BackupUploadChunkSize(chunk.Index, chunkBytes, declaredBytes)
	if !expiresAt.After(input.Now) {
		if err := expireBackupUpload(ctx, tx, input.Actor, chunk.SessionID, input.Now); err != nil {
			return model.BackupUploadChunk{}, false, err
		}
		return model.BackupUploadChunk{}, false, commitBackupUpload(tx, ErrBackupUploadClosed)
	}
	if chunk.Index < 0 || chunk.Index >= chunkCount || geometry != chunk.SizeBytes ||
		chunk.Offset != model.OffsetForBackupUploadChunk(chunk.Index, chunkBytes) {
		return model.BackupUploadChunk{}, false, ErrBackupUploadState
	}
	existing, err := readBackupUploadChunk(ctx, tx, chunk.SessionID, chunk.Index)
	if err != nil {
		return model.BackupUploadChunk{}, false, err
	}
	if existing != nil {
		if existing.Digest == chunk.Digest && existing.SizeBytes == chunk.SizeBytes {
			if err := tx.Commit(); err != nil {
				return model.BackupUploadChunk{}, false, fmt.Errorf("commit backup upload retry: %w", err)
			}
			return *existing, false, nil
		}
		if err := quarantineBackupUpload(ctx, tx, input, existing); err != nil {
			return model.BackupUploadChunk{}, false, err
		}
		return model.BackupUploadChunk{}, false, commitBackupUpload(tx, ErrBackupUploadChunkConflict)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO backup_upload_chunks (session_id, chunk_index,
		offset_bytes, size_bytes, digest, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		chunk.SessionID, chunk.Index, chunk.Offset, chunk.SizeBytes, chunk.Digest,
		chunk.CreatedAt); err != nil {
		return model.BackupUploadChunk{}, false, fmt.Errorf("insert backup upload chunk: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET received_bytes = received_bytes + ?, received_chunks = received_chunks + 1,
		updated_at = ? WHERE id = ?`, chunk.SizeBytes, input.Now, chunk.SessionID); err != nil {
		return model.BackupUploadChunk{}, false, fmt.Errorf("advance backup upload session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.BackupUploadChunk{}, false, fmt.Errorf("commit backup upload chunk: %w", err)
	}
	return chunk, true, nil
}

func scanBackupUploadChunk(scanner backupScanner) (model.BackupUploadChunk, error) {
	var chunk model.BackupUploadChunk
	err := scanner.Scan(&chunk.SessionID, &chunk.Index, &chunk.Offset, &chunk.SizeBytes,
		&chunk.Digest, &chunk.CreatedAt)
	return chunk, err
}

func readBackupUploadChunk(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	index int,
) (*model.BackupUploadChunk, error) {
	row := tx.QueryRowContext(ctx, `SELECT session_id, chunk_index, offset_bytes, size_bytes,
		digest, created_at FROM backup_upload_chunks WHERE session_id = ? AND chunk_index = ?`,
		sessionID, index)
	chunk, err := scanBackupUploadChunk(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup upload chunk: %w", err)
	}
	return &chunk, nil
}

func quarantineBackupUpload(
	ctx context.Context,
	tx *sql.Tx,
	input BackupUploadChunkAppend,
	existing *model.BackupUploadChunk,
) error {
	reason := fmt.Sprintf("chunk %d conflicts with recorded digest %s", existing.Index, existing.Digest)
	if _, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET state = 'quarantined', last_error = ?, updated_at = ? WHERE id = ?`,
		reason, input.Now, existing.SessionID); err != nil {
		return fmt.Errorf("quarantine backup upload session: %w", err)
	}
	return insertBackupUploadAudit(ctx, tx, input.Actor, "backup.upload.quarantined",
		existing.SessionID, map[string]string{"reason": reason})
}

func expireBackupUpload(
	ctx context.Context,
	tx *sql.Tx,
	actor BackupUploadActor,
	sessionID string,
	now time.Time,
) error {
	if _, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET state = 'expired', last_error = 'session expired', updated_at = ? WHERE id = ?`,
		now, sessionID); err != nil {
		return fmt.Errorf("expire backup upload session: %w", err)
	}
	return insertBackupUploadAudit(ctx, tx, actor, "backup.upload.expired", sessionID, nil)
}

func commitBackupUpload(tx *sql.Tx, target error) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup upload state: %w", err)
	}
	return target
}
