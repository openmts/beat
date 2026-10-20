package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	ErrThemePackageNotFound = errors.New("store: theme package not found")
	ErrThemePackageConflict = errors.New("store: theme package identity or digest conflict")
	ErrThemePackageActive   = errors.New("store: active theme package cannot be deleted")
	ErrThemeNoActivation    = errors.New("store: no theme activation exists")
	ErrThemeMarketNotFound  = errors.New("store: theme market source not found")
	ErrThemeMarketSourceID  = errors.New("store: theme market source id mismatch")
)

type ThemeStore struct {
	db *sql.DB
}

func NewThemeStore(db *sql.DB) *ThemeStore {
	return &ThemeStore{db: db}
}

const themePackageColumns = `id, theme_id, name, version, author, description, license,
	runtime, manifest_schema, minimum_beat_version, maximum_beat_version, archive_digest,
	archive_bytes, content_bytes, manifest_json, configuration_json, source_type, source_id,
	state, validated_at, creator_id, last_error_category, created_at, updated_at`

func scanThemePackage(scanner sessionScanner) (*model.ThemePackage, error) {
	var pkg model.ThemePackage
	var maxVersion sql.NullString
	var sourceID sql.NullString
	err := scanner.Scan(&pkg.ID, &pkg.ThemeID, &pkg.Name, &pkg.Version, &pkg.Author,
		&pkg.Description, &pkg.License, &pkg.Runtime, &pkg.ManifestSchema,
		&pkg.MinimumBeatVersion, &maxVersion, &pkg.ArchiveDigest, &pkg.ArchiveBytes,
		&pkg.ContentBytes, &pkg.ManifestJSON, &pkg.ConfigurationJSON, &pkg.SourceType,
		&sourceID, &pkg.State, &pkg.ValidatedAt, &pkg.CreatorID, &pkg.LastErrorCategory,
		&pkg.CreatedAt, &pkg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan theme package: %w", err)
	}
	pkg.MaximumBeatVersion = maxVersion.String
	pkg.SourceID = sourceID.String
	return &pkg, nil
}

func (store *ThemeStore) GetPackageByID(ctx context.Context, id string) (*model.ThemePackage, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+themePackageColumns+
		" FROM theme_packages WHERE id = ?", id)
	return scanThemePackage(row)
}

func (store *ThemeStore) GetPackageByDigest(ctx context.Context, digest string) (*model.ThemePackage, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+themePackageColumns+
		" FROM theme_packages WHERE archive_digest = ?", digest)
	return scanThemePackage(row)
}

func (store *ThemeStore) ListPackages(ctx context.Context) ([]model.ThemePackage, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+themePackageColumns+
		" FROM theme_packages WHERE state <> 'deleted' ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("list theme packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	packages := []model.ThemePackage{}
	for rows.Next() {
		pkg, scanErr := scanThemePackage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		packages = append(packages, *pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate theme packages: %w", err)
	}
	return packages, nil
}

func (store *ThemeStore) UpdatePackageState(
	ctx context.Context, id, state, errorCategory string, at time.Time,
) error {
	result, err := store.db.ExecContext(ctx, `UPDATE theme_packages SET state = ?,
		last_error_category = ?, updated_at = ? WHERE id = ?`, state, errorCategory, at, id)
	if err != nil {
		return fmt.Errorf("update theme package state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update theme package state rows affected: %w", err)
	}
	if affected == 0 {
		return ErrThemePackageNotFound
	}
	return nil
}

// MarkActivationUnhealthy 标记当前激活不健康。
func (store *ThemeStore) MarkActivationUnhealthy(ctx context.Context, category string, at time.Time) error {
	return store.mutate(ctx, themeWrite{"theme.unhealthy", "theme_activation", "current"}, func(mutation themeTransaction) error {
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_activations SET health_state='unhealthy',last_failure_category=? WHERE deactivated_at IS NULL`, category)
		return requireThemeRow(result, err, ErrThemeNoActivation)
	})
}

// --- 市场源 ---

const themeMarketColumns = `id, name, catalog_url, enabled, sort_order, public_key,
	key_fingerprint, cache_etag, cache_last_modified, fetched_at, expires_at, catalog_digest,
	last_status, last_error_category, created_at, updated_at`

func scanThemeMarket(scanner sessionScanner) (*model.ThemeMarketSource, error) {
	var source model.ThemeMarketSource
	err := scanner.Scan(&source.ID, &source.Name, &source.CatalogURL, &source.Enabled,
		&source.SortOrder, &source.PublicKey, &source.KeyFingerprint, &source.CacheETag,
		&source.CacheLastModified, &source.FetchedAt, &source.ExpiresAt, &source.CatalogDigest,
		&source.LastStatus, &source.LastErrorCategory, &source.CreatedAt, &source.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan theme market source: %w", err)
	}
	return &source, nil
}

func (store *ThemeStore) GetMarketSource(ctx context.Context, id string) (*model.ThemeMarketSource, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+themeMarketColumns+
		" FROM theme_market_sources WHERE id = ?", id)
	return scanThemeMarket(row)
}

func (store *ThemeStore) ListMarketSources(ctx context.Context) ([]model.ThemeMarketSource, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+themeMarketColumns+
		" FROM theme_market_sources ORDER BY sort_order, name")
	if err != nil {
		return nil, fmt.Errorf("list theme market sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sources := []model.ThemeMarketSource{}
	for rows.Next() {
		source, scanErr := scanThemeMarket(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sources = append(sources, *source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate theme market sources: %w", err)
	}
	return sources, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
