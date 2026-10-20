package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/beat/backend/internal/model"
)

func applyAdministratorChange(ctx context.Context, tx *sql.Tx, change AdministratorChange) (*model.AdminUser, error) {
	if change.Audit.Action == AdministratorCreate {
		return createAdministratorTx(ctx, tx, change)
	}
	current, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", change.User.ID))
	if err != nil {
		return nil, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if current == nil {
		return nil, ErrAdminUserNotFound
	}
	switch change.Audit.Action {
	case AdministratorDelete:
		if current.Enabled && current.Role == model.AdminRoleOwner {
			if err := ensureTransactionalOwner(ctx, tx, change); err != nil {
				return nil, err
			}
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM admin_users WHERE id=?", current.ID)
		if err := oneAdministratorWrite(result, err); err != nil {
			return nil, err
		}
		remaining, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", current.ID))
		if err != nil || remaining != nil {
			return nil, errors.Join(ErrAdminMutationUnavailable, err)
		}
		return nil, nil
	case AdministratorUpdate:
		return updateAdministratorTx(ctx, tx, administratorUpdate{change: change, current: *current})
	case AdministratorPassword:
		return passwordAdministratorTx(ctx, tx, administratorUpdate{change: change, current: *current})
	}
	return nil, ErrAdminMutationDenied
}

func createAdministratorTx(ctx context.Context, tx *sql.Tx, change AdministratorChange) (*model.AdminUser, error) {
	user := change.User
	user.Normalize()
	if err := user.Validate(); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_users(id,username,display_name,role,password_hash,enabled,password_changed_at,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?)`, user.ID, user.Username, user.DisplayName, user.Role, user.PasswordHash, boolToInt(user.Enabled), user.PasswordChangedAt, user.CreatedAt, user.UpdatedAt)
	if err != nil && strings.Contains(err.Error(), "admin_users.username") {
		return nil, ErrAdminUsernameConflict
	}
	if err := oneAdministratorWrite(result, err); err != nil {
		return nil, err
	}
	return readAdministratorResult(ctx, tx, user)
}

type administratorUpdate struct {
	change  AdministratorChange
	current model.AdminUser
}

func updateAdministratorTx(ctx context.Context, tx *sql.Tx, input administratorUpdate) (*model.AdminUser, error) {
	change, current := input.change, input.current
	next := current
	next.DisplayName, next.Role, next.Enabled = change.User.DisplayName, change.User.Role, change.User.Enabled
	next.Normalize()
	if err := next.Validate(); err != nil {
		return nil, err
	}
	if current.Enabled && current.Role == model.AdminRoleOwner && (next.Role != model.AdminRoleOwner || !next.Enabled) {
		if err := ensureTransactionalOwner(ctx, tx, change); err != nil {
			return nil, err
		}
	}
	next.UpdatedAt = administratorMutationTime(change)
	result, err := tx.ExecContext(ctx, "UPDATE admin_users SET display_name=?,role=?,enabled=?,updated_at=? WHERE id=?", next.DisplayName, next.Role, boolToInt(next.Enabled), next.UpdatedAt, next.ID)
	if err := oneAdministratorWrite(result, err); err != nil {
		return nil, err
	}
	if next.Role != current.Role || !next.Enabled {
		if err := invalidateAdministratorSessions(ctx, tx, change); err != nil {
			return nil, err
		}
	}
	return readAdministratorResult(ctx, tx, next)
}

func passwordAdministratorTx(ctx context.Context, tx *sql.Tx, input administratorUpdate) (*model.AdminUser, error) {
	change, current := input.change, input.current
	if current.PasswordHash != change.ExpectedPasswordHash {
		return nil, ErrAdminMutationConflict
	}
	next := current
	next.PasswordHash = change.User.PasswordHash
	next.PasswordChangedAt = administratorMutationTime(change)
	next.UpdatedAt = next.PasswordChangedAt
	result, err := tx.ExecContext(ctx, "UPDATE admin_users SET password_hash=?,password_changed_at=?,updated_at=? WHERE id=? AND password_hash=?", next.PasswordHash, next.PasswordChangedAt, next.UpdatedAt, next.ID, change.ExpectedPasswordHash)
	if err := oneAdministratorWrite(result, err); err != nil {
		return nil, err
	}
	if err := invalidateAdministratorSessions(ctx, tx, change); err != nil {
		return nil, err
	}
	return readAdministratorResult(ctx, tx, next)
}

func readAdministratorResult(ctx context.Context, tx *sql.Tx, expected model.AdminUser) (*model.AdminUser, error) {
	user, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", expected.ID))
	if err != nil {
		return nil, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if user == nil || user.Username != expected.Username || user.DisplayName != expected.DisplayName || user.Role != expected.Role || user.Enabled != expected.Enabled || user.PasswordHash != expected.PasswordHash || !user.PasswordChangedAt.Equal(expected.PasswordChangedAt) {
		return nil, ErrAdminMutationUnavailable
	}
	return user, nil
}

func oneAdministratorWrite(result sql.Result, err error) error {
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	return nil
}

func administratorAuditDetail(target string, fields []string) (string, error) {
	raw, err := json.Marshal(struct {
		Username string   `json:"target_username"`
		Fields   []string `json:"changed_fields"`
	}{target, fields})
	return string(raw), err
}

func prepareAdministratorAudit(ctx context.Context, tx *sql.Tx, change *AdministratorChange) error {
	username := change.User.Username
	if change.Audit.Action != AdministratorCreate {
		err := tx.QueryRowContext(ctx, "SELECT username FROM admin_users WHERE id=?", change.User.ID).Scan(&username)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdminUserNotFound
		}
		if err != nil {
			return errors.Join(ErrAdminMutationUnavailable, err)
		}
	}
	fields := map[string][]string{AdministratorCreate: {"username", "display_name", "role"}, AdministratorUpdate: {"display_name", "role", "enabled"}, AdministratorDelete: {"deleted"}, AdministratorPassword: {"password", "other_sessions"}}
	detail, err := administratorAuditDetail(username, fields[change.Audit.Action])
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	change.Audit.DetailJSON = detail
	return nil
}
