package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func (store *AdminStore) Bootstrap(
	ctx context.Context, user *model.AdminUser, session *model.AdminSession,
) error {
	store.bootstrapMu.Lock()
	defer store.bootstrapMu.Unlock()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_users").Scan(&count); err != nil {
		return fmt.Errorf("count administrators for bootstrap: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("administrator bootstrap is already complete")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_users (
		id, username, display_name, role, password_hash, enabled, password_changed_at,
		last_login_at, totp_secret_encrypted, totp_enabled_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, user.DisplayName,
		user.Role, user.PasswordHash, boolToInt(user.Enabled), user.PasswordChangedAt,
		user.LastLoginAt, user.TOTPSecretEncrypted, user.TOTPEnabledAt, user.CreatedAt, user.UpdatedAt); err != nil {
		return fmt.Errorf("create bootstrap owner: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_sessions (
		id, user_id, token_hash, token_prefix, created_at, last_activity_at, idle_expires_at,
		absolute_expires_at, reauthenticated_until, ip_address, user_agent, revoked_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.TokenHash,
		session.TokenPrefix, session.CreatedAt, session.LastActivityAt, session.IdleExpiresAt,
		session.AbsoluteExpiresAt, session.ReauthenticatedUntil, session.IPAddress,
		session.UserAgent, session.RevokedAt); err != nil {
		return fmt.Errorf("create bootstrap session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit administrator bootstrap: %w", err)
	}
	return nil
}
