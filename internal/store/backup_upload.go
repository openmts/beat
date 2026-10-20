package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var (
	ErrBackupUploadLimit  = errors.New("backup upload session limit reached")
	ErrBackupUploadState  = errors.New("backup upload session state does not allow the operation")
	ErrBackupUploadClosed = errors.New("backup upload session is not open")
)

// BackupUploadActor 携带会话写入审计所需的调用方元数据。
type BackupUploadActor struct {
	UserID, SessionID, RequestID, IPAddress, UserAgent, Username, SessionPrefix string
}

type BackupUploadStore struct {
	db *sql.DB
}

func NewBackupUploadStore(db *sql.DB) *BackupUploadStore {
	return &BackupUploadStore{db: db}
}

const backupUploadSessionColumns = `id, owner_user_id, filename, declared_bytes, declared_digest,
	chunk_bytes, chunk_count, received_bytes, received_chunks, staging_name, state, backup_id,
	last_error, staging_removed, expires_at, created_at, updated_at`

// Create 建立分块上传会话，并在同一事务内执行打开会话上限与审计写入。
func (store *BackupUploadStore) Create(
	ctx context.Context,
	actor BackupUploadActor,
	session *model.BackupUploadSession,
) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup upload session: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_upload_sessions
		WHERE owner_user_id = ? AND state IN ('open', 'quarantined')`,
		session.OwnerUserID).Scan(&open); err != nil {
		return fmt.Errorf("count open backup upload sessions: %w", err)
	}
	if open >= model.MaximumOpenBackupUploadsPerOwner {
		return ErrBackupUploadLimit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO backup_upload_sessions (`+
		backupUploadSessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '', 0, ?, ?, ?)`,
		session.ID, session.OwnerUserID, session.Filename, session.DeclaredBytes,
		session.DeclaredDigest, session.ChunkBytes, session.ChunkCount, session.ReceivedBytes,
		session.ReceivedChunks, session.StagingName, session.State, session.ExpiresAt,
		session.CreatedAt, session.UpdatedAt); err != nil {
		return fmt.Errorf("create backup upload session: %w", err)
	}
	if err := insertBackupUploadAudit(ctx, tx, actor, "backup.upload.create", session.ID, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup upload session: %w", err)
	}
	return nil
}

// Get 返回会话；不存在时返回 (nil, nil)。
func (store *BackupUploadStore) Get(ctx context.Context, id string) (*model.BackupUploadSession, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+backupUploadSessionColumns+
		" FROM backup_upload_sessions WHERE id = ?", id)
	session, err := scanBackupUploadSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get backup upload session: %w", err)
	}
	return &session, nil
}

// ListByOwner 返回该管理员最近的会话，最新的在前。
func (store *BackupUploadStore) ListByOwner(
	ctx context.Context,
	ownerUserID string,
	limit int,
) ([]model.BackupUploadSession, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+backupUploadSessionColumns+` FROM backup_upload_sessions
		WHERE owner_user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, ownerUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list backup upload sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sessions := []model.BackupUploadSession{}
	for rows.Next() {
		session, scanErr := scanBackupUploadSession(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan backup upload session: %w", scanErr)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup upload sessions: %w", err)
	}
	return sessions, nil
}

// ListQuarantined 返回被拒的上传会话，最新的在前；与备份归档隔离清单同源可见，
// 让运维不必查库或翻自己的会话列表才能看到被隔离的会话。
func (store *BackupUploadStore) ListQuarantined(
	ctx context.Context,
	limit int,
) ([]model.BackupUploadSession, error) {
	if limit < 1 || limit > BackupMaximumPage {
		return nil, ErrInvalidBackupQuery
	}
	rows, err := store.db.QueryContext(ctx, "SELECT "+backupUploadSessionColumns+` FROM backup_upload_sessions
		WHERE state = 'quarantined' ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list quarantined backup upload sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sessions := []model.BackupUploadSession{}
	for rows.Next() {
		session, scanErr := scanBackupUploadSession(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan quarantined backup upload session: %w", scanErr)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quarantined backup upload sessions: %w", err)
	}
	return sessions, nil
}

// SetState 只在会话仍可写时迁移状态，并记录原因。
func (store *BackupUploadStore) SetState(
	ctx context.Context,
	actor BackupUploadActor,
	id, state, reason string,
	now time.Time,
) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup upload transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET state = ?, last_error = ?, updated_at = ?
		WHERE id = ? AND state IN ('open', 'quarantined')`, state, reason, now, id)
	if err != nil {
		return fmt.Errorf("update backup upload session: %w", err)
	}
	if err := requireAffected(result, "backup upload session"); err != nil {
		return ErrBackupUploadState
	}
	detail := map[string]string{"state": state}
	if err := insertBackupUploadAudit(ctx, tx, actor, "backup.upload."+state, id, detail); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup upload transition: %w", err)
	}
	return nil
}

// MarkValidated 把已完成校验的会话绑定到备份记录。
func (store *BackupUploadStore) MarkValidated(
	ctx context.Context,
	actor BackupUploadActor,
	id, backupID string,
	now time.Time,
) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup upload validation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE backup_upload_sessions
		SET state = 'validated', backup_id = ?, last_error = '', updated_at = ?
		WHERE id = ? AND state = 'open'`, backupID, now, id)
	if err != nil {
		return fmt.Errorf("validate backup upload session: %w", err)
	}
	if err := requireAffected(result, "backup upload session"); err != nil {
		return ErrBackupUploadClosed
	}
	if err := insertBackupUploadAudit(ctx, tx, actor, "backup.upload.validated", id,
		map[string]string{"backup_id": backupID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup upload validation: %w", err)
	}
	return nil
}

func scanBackupUploadSession(scanner backupScanner) (model.BackupUploadSession, error) {
	var session model.BackupUploadSession
	var backupID sql.NullString
	err := scanner.Scan(&session.ID, &session.OwnerUserID, &session.Filename,
		&session.DeclaredBytes, &session.DeclaredDigest, &session.ChunkBytes, &session.ChunkCount,
		&session.ReceivedBytes, &session.ReceivedChunks, &session.StagingName, &session.State,
		&backupID, &session.LastError, &session.StagingRemoved, &session.ExpiresAt,
		&session.CreatedAt, &session.UpdatedAt)
	session.BackupID = backupID.String
	return session, err
}

func insertBackupUploadAudit(
	ctx context.Context,
	database schemaExecutor,
	actor BackupUploadActor,
	action, sessionID string,
	detail map[string]string,
) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode backup upload audit: %w", err)
	}
	event := model.AdminAuditEvent{ID: uuid.NewString(), RequestID: actor.RequestID,
		ActorID: actor.UserID, ActorUsername: actor.Username, Action: action,
		ResourceType: "backup_upload_session", ResourceID: sessionID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: string(encoded),
		IPAddress: actor.IPAddress, UserAgent: actor.UserAgent,
		SessionPrefix: actor.SessionPrefix, CreatedAt: time.Now().UTC()}
	return insertAuditEvent(ctx, database, &event)
}
