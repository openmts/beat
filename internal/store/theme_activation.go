package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// ActivatePackage 同一事务重查包状态、切换当前激活、写站点指针及审计。
func (store *ThemeStore) ActivatePackage(ctx context.Context, activation *model.ThemeActivation, at time.Time) error {
	write := themeWrite{"theme." + activation.Reason, "theme_package", activation.PackageID}
	return store.mutate(ctx, write, func(mutation themeTransaction) error {
		if err := mutation.prepareActivation(activation); err != nil {
			return err
		}
		if _, err := mutation.tx.ExecContext(ctx, `UPDATE theme_activations SET deactivated_at=? WHERE deactivated_at IS NULL`, at); err != nil {
			return err
		}
		if _, err := mutation.tx.ExecContext(ctx, `INSERT INTO theme_activations (
			id,package_id,previous_package_id,actor_id,reason,activated_at,deactivated_at,health_state,last_failure_category
		) VALUES(?,?,?,?,?,?,NULL,'healthy','')`, activation.ID, nullableString(activation.PackageID),
			nullableString(activation.PreviousPackageID), activation.ActorID, activation.Reason, at); err != nil {
			return err
		}
		result, err := mutation.tx.ExecContext(ctx, `UPDATE site_settings SET active_theme_package_id=?`, nullableString(activation.PackageID))
		return requireThemeRow(result, err, ErrThemeNoActivation)
	})
}

func (mutation themeTransaction) prepareActivation(activation *model.ThemeActivation) error {
	if activation.PackageID != "" {
		var state string
		err := mutation.tx.QueryRowContext(mutation.ctx, `SELECT state FROM theme_packages WHERE id=?`, activation.PackageID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrThemePackageNotFound
		}
		if err != nil {
			return err
		}
		if state != model.ThemeStateInstalled {
			return ErrThemePackageNotFound
		}
	}
	current, err := readThemeActivation(mutation.ctx, mutation.tx)
	if err != nil {
		return err
	}
	activation.PreviousPackageID = ""
	if current != nil {
		activation.PreviousPackageID = current.PackageID
	}
	return nil
}

type themeRowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readThemeActivation(ctx context.Context, database themeRowReader) (*model.ThemeActivation, error) {
	row := database.QueryRowContext(ctx, `SELECT id,package_id,previous_package_id,actor_id,
		reason,activated_at,deactivated_at,health_state,last_failure_category FROM theme_activations
		WHERE deactivated_at IS NULL ORDER BY activated_at DESC,rowid DESC LIMIT 1`)
	var activation model.ThemeActivation
	var pkgID, previous sql.NullString
	err := row.Scan(&activation.ID, &pkgID, &previous, &activation.ActorID, &activation.Reason,
		&activation.ActivatedAt, &activation.DeactivatedAt, &activation.HealthState, &activation.LastFailureCategory)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan current theme activation: %w", err)
	}
	activation.PackageID, activation.PreviousPackageID = pkgID.String, previous.String
	return &activation, nil
}

func (store *ThemeStore) CurrentActivation(ctx context.Context) (*model.ThemeActivation, error) {
	return readThemeActivation(ctx, store.db)
}

// HealthyRollbackCandidates 仅返回每个包最近一次激活仍健康的已安装版本。
func (store *ThemeStore) HealthyRollbackCandidates(ctx context.Context, currentID string) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT h.package_id FROM theme_activations h
		JOIN theme_packages p ON p.id=h.package_id WHERE h.package_id<>? AND p.state='installed'
		AND h.health_state='healthy' AND NOT EXISTS(SELECT 1 FROM theme_activations n
		WHERE n.package_id=h.package_id AND (n.activated_at>h.activated_at OR
		(n.activated_at=h.activated_at AND n.rowid>h.rowid))) ORDER BY h.activated_at DESC,h.rowid DESC LIMIT 32`, currentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
