package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const adminSessionColumns = `id, user_id, token_hash, token_prefix, created_at,
	last_activity_at, idle_expires_at, absolute_expires_at, reauthenticated_until,
	ip_address, user_agent, revoked_at`

func (store *AdminStore) CreateSession(ctx context.Context, session *model.AdminSession) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_sessions (
		id, user_id, token_hash, token_prefix, created_at, last_activity_at, idle_expires_at,
		absolute_expires_at, reauthenticated_until, ip_address, user_agent, revoked_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.UserID,
		session.TokenHash, session.TokenPrefix, session.CreatedAt, session.LastActivityAt,
		session.IdleExpiresAt, session.AbsoluteExpiresAt, session.ReauthenticatedUntil,
		session.IPAddress, session.UserAgent, session.RevokedAt)
	if err != nil {
		return fmt.Errorf("create administrator session: %w", err)
	}
	return nil
}

func (store *AdminStore) GetSessionByTokenHash(ctx context.Context, hash []byte) (*model.AdminSession, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE token_hash = ?", hash)
	return scanAdminSession(row)
}

func (store *AdminStore) GetSessionByID(ctx context.Context, id string) (*model.AdminSession, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id = ?", id)
	return scanAdminSession(row)
}

func (store *AdminStore) ListSessions(ctx context.Context, userID string) ([]model.AdminSession, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+adminSessionColumns+
		" FROM admin_sessions WHERE user_id = ? ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, fmt.Errorf("list administrator sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sessions := []model.AdminSession{}
	for rows.Next() {
		session, scanErr := scanAdminSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrator sessions: %w", err)
	}
	return sessions, nil
}

func (store *AdminStore) TouchSession(ctx context.Context, id string, activity, idleExpiry time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_sessions SET last_activity_at = ?,
		idle_expires_at = ? WHERE id = ? AND revoked_at IS NULL`, activity, idleExpiry, id)
	return sessionRowsAffected(result, err, "touch administrator session")
}

func (store *AdminStore) MarkSessionReauthenticated(ctx context.Context, id string, until time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_sessions SET reauthenticated_until = ?
		WHERE id = ? AND revoked_at IS NULL`, until, id)
	return sessionRowsAffected(result, err, "reauthenticate administrator session")
}

func (store *AdminStore) RevokeSession(ctx context.Context, id string, at time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL`, at, id)
	return sessionRowsAffected(result, err, "revoke administrator session")
}

func (store *AdminStore) RevokeOtherSessions(ctx context.Context, userID, currentID string, at time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at = ?
		WHERE user_id = ? AND id <> ? AND revoked_at IS NULL`, at, userID, currentID)
	if err != nil {
		return 0, fmt.Errorf("revoke other administrator sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("revoke other administrator sessions rows affected: %w", err)
	}
	return count, nil
}

func (store *AdminStore) RevokeUserSessions(ctx context.Context, userID, exceptID string, at time.Time) error {
	_, err := store.RevokeOtherSessions(ctx, userID, exceptID, at)
	return err
}

func (store *AdminStore) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE
		absolute_expires_at <= ? OR idle_expires_at <= ? OR (revoked_at IS NOT NULL AND revoked_at <= ?)`,
		now, now, now.Add(-30*24*time.Hour))
	if err != nil {
		return 0, fmt.Errorf("delete expired administrator sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired administrator sessions rows affected: %w", err)
	}
	return count, nil
}

type sessionScanner interface {
	Scan(...any) error
}

func scanAdminSession(scanner sessionScanner) (*model.AdminSession, error) {
	var session model.AdminSession
	err := scanner.Scan(&session.ID, &session.UserID, &session.TokenHash, &session.TokenPrefix,
		&session.CreatedAt, &session.LastActivityAt, &session.IdleExpiresAt,
		&session.AbsoluteExpiresAt, &session.ReauthenticatedUntil, &session.IPAddress,
		&session.UserAgent, &session.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan administrator session: %w", err)
	}
	return &session, nil
}

func sessionRowsAffected(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if affected == 0 {
		return ErrAdminSessionNotFound
	}
	return nil
}
