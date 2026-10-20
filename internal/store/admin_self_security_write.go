package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/beat/backend/internal/model"
)

func applySelfSecurity(ctx context.Context, tx *sql.Tx, change SelfSecurityChange) (int64, error) {
	switch change.Audit.Action {
	case AdministratorSessionRevoke, AdministratorSessionsRevoke:
		return revokeSelfSessions(ctx, tx, change)
	case AdministratorTOTPBegin:
		return 0, nil
	default:
		return 0, writeSelfTOTP(ctx, tx, change)
	}
}

func revokeSelfSessions(ctx context.Context, tx *sql.Tx, change SelfSecurityChange) (int64, error) {
	predicate, target := "user_id=? AND id<>? AND revoked_at IS NULL", change.Principal.Session.ID
	single := change.Audit.Action == AdministratorSessionRevoke
	if single {
		predicate, target = "user_id=? AND id=? AND revoked_at IS NULL", change.Audit.ResourceID
	}
	var expected int64
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_sessions WHERE "+predicate, change.Principal.User.ID, target).Scan(&expected)
	if err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if single && expected != 1 {
		return 0, ErrAdminSessionNotFound
	}
	result, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=?,reauthenticated_until=NULL WHERE "+predicate, change.Audit.CreatedAt, change.Principal.User.ID, target)
	if err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != expected {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	var remaining int64
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_sessions WHERE "+predicate, change.Principal.User.ID, target).Scan(&remaining)
	if err != nil || remaining != 0 {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	return count, nil
}

func writeSelfTOTP(ctx context.Context, tx *sql.Tx, change SelfSecurityChange) error {
	user, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", change.Principal.User.ID))
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if user == nil || user.PasswordHash != change.Principal.User.PasswordHash {
		return ErrAdminMutationDenied
	}
	var secret []byte
	var enabled any
	if change.Audit.Action == AdministratorTOTPEnable {
		if !bytes.Equal(user.TOTPSecretEncrypted, change.PreviousSecret) || !sameOptionalTime(user.TOTPEnabledAt, change.PreviousEnabledAt) {
			return ErrAdminMutationConflict
		}
		secret, enabled = change.Secret, change.Audit.CreatedAt
	}
	result, err := tx.ExecContext(ctx, "UPDATE admin_users SET totp_secret_encrypted=?,totp_enabled_at=?,updated_at=? WHERE id=?", secret, enabled, change.Audit.CreatedAt, user.ID)
	if err := oneAdministratorWrite(result, err); err != nil {
		return err
	}
	updated, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", user.ID))
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if !selfTOTPResult(updated, change) {
		return ErrAdminMutationUnavailable
	}
	return nil
}

func selfTOTPResult(user *model.AdminUser, change SelfSecurityChange) bool {
	if user == nil {
		return false
	}
	if change.Audit.Action == AdministratorTOTPDisable {
		return len(user.TOTPSecretEncrypted) == 0 && user.TOTPEnabledAt == nil
	}
	return bytes.Equal(user.TOTPSecretEncrypted, change.Secret) && sameOptionalTime(user.TOTPEnabledAt, &change.Audit.CreatedAt)
}

func selfSecurityDetail(change SelfSecurityChange, count int64) (string, error) {
	fields := map[string][]string{AdministratorSessionRevoke: {"revoked_at"}, AdministratorSessionsRevoke: {"other_sessions"}, AdministratorTOTPBegin: {"pending_setup"}, AdministratorTOTPEnable: {"totp_enabled"}, AdministratorTOTPDisable: {"totp_disabled"}}
	data, err := json.Marshal(struct {
		Username string   `json:"target_username"`
		Fields   []string `json:"changed_fields"`
		Revoked  int64    `json:"revoked_count,omitempty"`
	}{change.Principal.User.Username, fields[change.Audit.Action], count})
	return string(data), err
}
