package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

func insertBootstrapUser(ctx context.Context, tx *sql.Tx, user model.AdminUser) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_users").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("administrator bootstrap is already complete")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_users (
  id,username,display_name,role,password_hash,enabled,password_changed_at,last_login_at,totp_secret_encrypted,totp_enabled_at,created_at,updated_at
 ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, user.ID, user.Username, user.DisplayName, user.Role, user.PasswordHash, boolToInt(user.Enabled), user.PasswordChangedAt, user.LastLoginAt, user.TOTPSecretEncrypted, user.TOTPEnabledAt, user.CreatedAt, user.UpdatedAt)
	return sessionRowsAffected(result, err, "insert bootstrap owner")
}

func insertLocalSession(ctx context.Context, tx *sql.Tx, s model.AdminSession) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_sessions (
  id,user_id,token_hash,token_prefix,created_at,last_activity_at,idle_expires_at,absolute_expires_at,reauthenticated_until,ip_address,user_agent,revoked_at,authentication_method,external_identity_id
 ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.ID, s.UserID, s.TokenHash, s.TokenPrefix, s.CreatedAt, s.LastActivityAt, s.IdleExpiresAt, s.AbsoluteExpiresAt, nil, s.IPAddress, s.UserAgent, nil, model.AuthMethodLocal, nil)
	return sessionRowsAffected(result, err, "insert local session")
}
