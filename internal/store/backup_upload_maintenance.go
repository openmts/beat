package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// Expire 把超过有效期且仍可写的会话标记为 expired，并保留暂存目录等待清理。
func (store *BackupUploadStore) Expire(ctx context.Context, now time.Time) (int, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin backup upload expiry: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM backup_upload_sessions
		WHERE state IN ('open', 'quarantined') AND expires_at <= ? ORDER BY expires_at LIMIT 256`, now)
	if err != nil {
		return 0, fmt.Errorf("select expired backup upload sessions: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan expired backup upload session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired backup upload sessions: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions SET state = 'expired',
			last_error = 'session expired', updated_at = ? WHERE id = ?`, now, id); err != nil {
			return 0, fmt.Errorf("expire backup upload session: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit_events(id, request_id, actor_id,
			actor_username, action, resource_type, resource_id, outcome, detail_json, ip_address,
			user_agent, session_prefix, created_at) VALUES(?, '', NULL, 'system',
			'backup.upload.expired', 'backup_upload_session', ?, 'success', '{}', '', '', '', ?)`,
			uuid.NewString(), id, now); err != nil {
			return 0, fmt.Errorf("audit expired backup upload session: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit backup upload expiry: %w", err)
	}
	return len(ids), nil
}

// PendingStagingCleanup 返回尚未移除私有暂存目录的终态会话。
func (store *BackupUploadStore) PendingStagingCleanup(
	ctx context.Context,
	limit int,
) ([]model.BackupUploadSession, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+backupUploadSessionColumns+`
		FROM backup_upload_sessions WHERE staging_removed = 0
		AND state IN ('validated', 'cancelled', 'expired')
		ORDER BY updated_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list backup upload staging cleanup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sessions := []model.BackupUploadSession{}
	for rows.Next() {
		session, scanErr := scanBackupUploadSession(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan backup upload staging cleanup: %w", scanErr)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup upload staging cleanup: %w", err)
	}
	return sessions, nil
}

// MarkStagingRemoved 记录该会话的私有暂存目录已删除。
func (store *BackupUploadStore) MarkStagingRemoved(
	ctx context.Context,
	id string,
	now time.Time,
) error {
	result, err := store.db.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET staging_removed = 1, updated_at = ? WHERE id = ? AND staging_removed = 0`, now, id)
	if err != nil {
		return fmt.Errorf("mark backup upload staging removed: %w", err)
	}
	return requireAffected(result, "backup upload session")
}

// Prune 在每个管理员的历史保留上限之外删除已清理的终态会话及分块。
func (store *BackupUploadStore) Prune(
	ctx context.Context,
	keepPerOwner int,
	limit int,
) (int, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin backup upload prune: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM (
		SELECT id, ROW_NUMBER() OVER (PARTITION BY owner_user_id ORDER BY created_at DESC, id DESC)
			AS position FROM backup_upload_sessions WHERE staging_removed = 1
	) WHERE position > ? ORDER BY position LIMIT ?`, keepPerOwner, limit)
	if err != nil {
		return 0, fmt.Errorf("select prunable backup upload sessions: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan prunable backup upload session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close prunable backup upload sessions: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "DELETE FROM backup_upload_chunks WHERE session_id = ?", id); err != nil {
			return 0, fmt.Errorf("delete pruned backup upload chunks: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM backup_upload_sessions WHERE id = ?", id); err != nil {
			return 0, fmt.Errorf("delete pruned backup upload session: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit backup upload prune: %w", err)
	}
	return len(ids), nil
}
