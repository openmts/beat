package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

func (store *ThemeStore) CreatePackage(ctx context.Context, pkg *model.ThemePackage) error {
	return store.mutate(ctx, themeWrite{"theme.install", "theme_package", pkg.ID}, func(mutation themeTransaction) error {
		if err := mutation.validateMarketBinding(); err != nil {
			return err
		}
		if err := mutation.requireNewPackage(pkg); err != nil {
			return err
		}
		_, err := mutation.tx.ExecContext(ctx, `INSERT INTO theme_packages (
			id, theme_id, name, version, author, description, license, runtime, manifest_schema,
			minimum_beat_version, maximum_beat_version, archive_digest, archive_bytes, content_bytes,
			manifest_json, configuration_json, source_type, source_id, state, validated_at,
			creator_id, last_error_category, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pkg.ID, pkg.ThemeID, pkg.Name, pkg.Version, pkg.Author, pkg.Description, pkg.License,
			pkg.Runtime, pkg.ManifestSchema, pkg.MinimumBeatVersion, nullableString(pkg.MaximumBeatVersion),
			pkg.ArchiveDigest, pkg.ArchiveBytes, pkg.ContentBytes, pkg.ManifestJSON, pkg.ConfigurationJSON,
			pkg.SourceType, nullableString(pkg.SourceID), pkg.State, pkg.ValidatedAt, pkg.CreatorID,
			pkg.LastErrorCategory, pkg.CreatedAt, pkg.UpdatedAt)
		return err
	})
}

func (mutation themeTransaction) requireNewPackage(pkg *model.ThemePackage) error {
	var exists bool
	err := mutation.tx.QueryRowContext(mutation.ctx, `SELECT EXISTS(SELECT 1 FROM theme_packages
		WHERE archive_digest=? OR (theme_id=? AND version=?))`, pkg.ArchiveDigest, pkg.ThemeID, pkg.Version).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return ErrThemePackageConflict
	}
	return nil
}

// RestorePackage 仅恢复同一内容身份，不修改配置、来源或激活记录。
func (store *ThemeStore) RestorePackage(ctx context.Context, pkg *model.ThemePackage) error {
	return store.mutate(ctx, themeWrite{"theme.restore", "theme_package", pkg.ID}, func(mutation themeTransaction) error {
		if err := mutation.validateMarketBinding(); err != nil {
			return err
		}
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_packages SET state='installed',
			name=?,author=?,description=?,license=?,manifest_schema=?,minimum_beat_version=?,maximum_beat_version=?,
			archive_bytes=?,content_bytes=?,manifest_json=?,validated_at=?,updated_at=?,last_error_category=''
			WHERE id=? AND state='deleted' AND archive_digest=? AND theme_id=? AND version=? AND runtime=?`,
			pkg.Name, pkg.Author, pkg.Description, pkg.License, pkg.ManifestSchema, pkg.MinimumBeatVersion,
			nullableString(pkg.MaximumBeatVersion), pkg.ArchiveBytes, pkg.ContentBytes, pkg.ManifestJSON, pkg.ValidatedAt, pkg.UpdatedAt,
			pkg.ID, pkg.ArchiveDigest, pkg.ThemeID, pkg.Version, pkg.Runtime)
		return requireThemeRow(result, err, ErrThemePackageConflict)
	})
}

// DeletePackage 先原子落盘逻辑删除及审计；资产层立即拒绝 deleted 包。
func (store *ThemeStore) DeletePackage(ctx context.Context, id string, at time.Time) (*model.ThemePackage, error) {
	var pkg *model.ThemePackage
	err := store.mutate(ctx, themeWrite{"theme.delete", "theme_package", id}, func(mutation themeTransaction) error {
		var err error
		pkg, err = scanThemePackage(mutation.tx.QueryRowContext(ctx,
			"SELECT "+themePackageColumns+" FROM theme_packages WHERE id=?", id))
		if err != nil {
			return err
		}
		if pkg == nil {
			return ErrThemePackageNotFound
		}
		if err := mutation.requireInactive(id); err != nil {
			return err
		}
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_packages SET state='deleted',
			last_error_category='quarantine_pending',updated_at=? WHERE id=?`, at, id)
		return requireThemeRow(result, err, ErrThemePackageNotFound)
	})
	return pkg, err
}

func (mutation themeTransaction) requireInactive(id string) error {
	var active bool
	err := mutation.tx.QueryRowContext(mutation.ctx, `SELECT
		EXISTS(SELECT 1 FROM theme_activations WHERE package_id=? AND deactivated_at IS NULL)
		OR EXISTS(SELECT 1 FROM site_settings WHERE active_theme_package_id=?)`, id, id).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return ErrThemePackageActive
	}
	return nil
}

func (store *ThemeStore) CompletePackageDeletion(ctx context.Context, id string, at time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE theme_packages SET last_error_category='deleted',
		updated_at=? WHERE id=? AND state='deleted'`, at, id)
	return requireThemeRow(result, err, ErrThemePackageNotFound)
}

func (store *ThemeStore) ListDeletedPackages(ctx context.Context) ([]model.ThemePackage, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+themePackageColumns+" FROM theme_packages WHERE state='deleted'")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	packages := []model.ThemePackage{}
	for rows.Next() {
		pkg, err := scanThemePackage(rows)
		if err != nil {
			return nil, err
		}
		packages = append(packages, *pkg)
	}
	return packages, rows.Err()
}

func requireThemeRow(result sql.Result, err error, missing error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("theme mutation rows affected: %w", err)
	}
	if count != 1 {
		return missing
	}
	return nil
}
