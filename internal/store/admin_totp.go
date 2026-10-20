package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type TOTPActivation struct {
	UserID, SessionID      string
	Secret, PreviousSecret []byte
	PreviousEnabledAt      *time.Time
	Now                    time.Time
}

// 原因子与授权会话在同一个 UPDATE 内校验，跨进程并发也不能覆盖新因子。
func (store *AdminStore) ConfirmTOTP(ctx context.Context, input TOTPActivation) (bool, error) {
	if input.UserID == "" || input.SessionID == "" || len(input.Secret) == 0 || input.Now.IsZero() {
		return false, errors.New("invalid TOTP activation")
	}
	now := input.Now.UTC()
	result, err := store.db.ExecContext(ctx, `UPDATE admin_users
		SET totp_secret_encrypted = ?, totp_enabled_at = ?, updated_at = ?
		WHERE id = ? AND enabled = 1 AND totp_secret_encrypted IS ? AND totp_enabled_at IS ?
		AND EXISTS (SELECT 1 FROM admin_sessions WHERE id = ? AND user_id = admin_users.id
			AND revoked_at IS NULL AND idle_expires_at > ? AND absolute_expires_at > ?
			AND reauthenticated_until > ?)`, input.Secret, now, now, input.UserID,
		input.PreviousSecret, input.PreviousEnabledAt, input.SessionID, now, now, now)
	if err != nil {
		return false, fmt.Errorf("confirm administrator TOTP: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("confirm administrator TOTP result: %w", err)
	}
	return changed == 1, nil
}
