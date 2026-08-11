package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const adminUserColumns = `id, username, display_name, role, password_hash, enabled,
	password_changed_at, last_login_at, totp_secret_encrypted, totp_enabled_at, created_at, updated_at`

func (store *AdminStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_users").Scan(&count); err != nil {
		return 0, fmt.Errorf("count administrators: %w", err)
	}
	return count, nil
}

func (store *AdminStore) CreateUser(ctx context.Context, user *model.AdminUser) error {
	user.Normalize()
	if err := user.Validate(); err != nil {
		return err
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_users (
		id, username, display_name, role, password_hash, enabled, password_changed_at,
		last_login_at, totp_secret_encrypted, totp_enabled_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username,
		user.DisplayName, user.Role, user.PasswordHash, boolToInt(user.Enabled), user.PasswordChangedAt,
		user.LastLoginAt, user.TOTPSecretEncrypted, user.TOTPEnabledAt, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create administrator: %w", err)
	}
	return nil
}

func (store *AdminStore) GetUserByUsername(ctx context.Context, username string) (*model.AdminUser, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE username = ?",
		model.NormalizeUsername(username))
	return scanAdminUser(row)
}

func (store *AdminStore) GetUserByID(ctx context.Context, id string) (*model.AdminUser, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id = ?", id)
	return scanAdminUser(row)
}

func (store *AdminStore) ListUsers(ctx context.Context) ([]model.AdminUser, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users ORDER BY created_at, username")
	if err != nil {
		return nil, fmt.Errorf("list administrators: %w", err)
	}
	defer func() { _ = rows.Close() }()
	users := []model.AdminUser{}
	for rows.Next() {
		user, scanErr := scanAdminUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrators: %w", err)
	}
	return users, nil
}

func (store *AdminStore) UpdateUser(
	ctx context.Context, id, displayName string, role model.AdminRole, enabled bool,
) error {
	store.userMu.Lock()
	defer store.userMu.Unlock()
	user, err := store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrAdminUserNotFound
	}
	if err := store.ensureOwnerRemains(ctx, user, role, enabled); err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx, `UPDATE admin_users SET display_name = ?, role = ?,
		enabled = ?, updated_at = ? WHERE id = ?`, displayName, role, boolToInt(enabled), model.NowUTC(), id)
	return adminRowsAffected(result, err, "update administrator")
}

func (store *AdminStore) DeleteUser(ctx context.Context, id string) error {
	store.userMu.Lock()
	defer store.userMu.Unlock()
	user, err := store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrAdminUserNotFound
	}
	if err := store.ensureOwnerRemains(ctx, user, model.AdminRoleAdmin, false); err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx, "DELETE FROM admin_users WHERE id = ?", id)
	return adminRowsAffected(result, err, "delete administrator")
}

func (store *AdminStore) UpdatePassword(ctx context.Context, id, passwordHash string, changedAt time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ?,
		password_changed_at = ?, updated_at = ? WHERE id = ?`, passwordHash, changedAt, changedAt, id)
	return adminRowsAffected(result, err, "update administrator password")
}

func (store *AdminStore) UpdateTOTP(
	ctx context.Context, id string, encrypted []byte, enabledAt *time.Time,
) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_users SET totp_secret_encrypted = ?,
		totp_enabled_at = ?, updated_at = ? WHERE id = ?`, encrypted, enabledAt, model.NowUTC(), id)
	return adminRowsAffected(result, err, "update administrator TOTP")
}

func (store *AdminStore) TouchUserLogin(ctx context.Context, id string, at time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = ?,
		updated_at = ? WHERE id = ?`, at, at, id)
	return adminRowsAffected(result, err, "touch administrator login")
}

func (store *AdminStore) ensureOwnerRemains(
	ctx context.Context, current *model.AdminUser, role model.AdminRole, enabled bool,
) error {
	if current.Role != model.AdminRoleOwner || !current.Enabled || (role == model.AdminRoleOwner && enabled) {
		return nil
	}
	var count int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users
		WHERE role = 'owner' AND enabled = 1 AND id <> ?`, current.ID).Scan(&count)
	if err != nil {
		return fmt.Errorf("count remaining owners: %w", err)
	}
	if count == 0 {
		return ErrLastOwner
	}
	return nil
}

type adminScanner interface {
	Scan(...any) error
}

func scanAdminUser(scanner adminScanner) (*model.AdminUser, error) {
	var user model.AdminUser
	var enabled int
	err := scanner.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.PasswordHash,
		&enabled, &user.PasswordChangedAt, &user.LastLoginAt, &user.TOTPSecretEncrypted,
		&user.TOTPEnabledAt, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan administrator: %w", err)
	}
	user.Enabled = enabled == 1
	return &user, nil
}

func adminRowsAffected(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if affected == 0 {
		return ErrAdminUserNotFound
	}
	return nil
}
