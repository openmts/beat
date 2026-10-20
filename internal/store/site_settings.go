package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

type SiteSettingsStore struct {
	db *sql.DB
}

func NewSiteSettingsStore(db *sql.DB) *SiteSettingsStore {
	return &SiteSettingsStore{db: db}
}

func (store *SiteSettingsStore) Get(ctx context.Context) (model.SiteSettings, error) {
	return scanSiteSettings(store.db.QueryRowContext(ctx, siteSettingsSelect))
}

const siteSettingsSelect = `SELECT site_title,site_description,logo_url,favicon_url,default_theme,public_address_mode,show_network_quality,show_accelerators,updated_at FROM site_settings WHERE id=1`

func scanSiteSettings(row rowScanner) (model.SiteSettings, error) {
	var settings model.SiteSettings
	err := row.Scan(&settings.SiteTitle, &settings.SiteDescription, &settings.LogoURL, &settings.FaviconURL, &settings.DefaultTheme, &settings.PublicAddressMode, &settings.ShowNetworkQuality, &settings.ShowAccelerators, &settings.UpdatedAt)
	return settings, err
}

func (store *SiteSettingsStore) Update(
	ctx context.Context,
	settings model.SiteSettings,
) (model.SiteSettings, error) {
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return model.SiteSettings{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return model.SiteSettings{}, err
	}
	before, err := scanSiteSettings(tx.QueryRowContext(ctx, siteSettingsSelect))
	if err != nil {
		return model.SiteSettings{}, err
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return model.SiteSettings{}, err
	}
	settings.UpdatedAt = model.NowUTC()
	if err := writeSiteSettings(ctx, tx, settings); err != nil {
		return model.SiteSettings{}, err
	}

	if err := mutation.finish(siteSettingsEvidence(before, settings)); err != nil {
		return model.SiteSettings{}, err
	}
	actual, err := scanSiteSettings(tx.QueryRowContext(ctx, siteSettingsSelect))
	if err != nil {
		return model.SiteSettings{}, err
	}
	if actual != settings {
		return model.SiteSettings{}, errors.New("site settings were not persisted")
	}
	if err := tx.Commit(); err != nil {
		return model.SiteSettings{}, err
	}
	return settings, nil
}

func writeSiteSettings(ctx context.Context, tx *sql.Tx, settings model.SiteSettings) error {
	result, err := tx.ExecContext(ctx, `UPDATE site_settings SET site_title = ?,
		site_description = ?, logo_url = ?, favicon_url = ?, default_theme = ?,
		public_address_mode = ?, show_network_quality = ?, show_accelerators = ?, updated_at = ? WHERE id = 1`,
		settings.SiteTitle, settings.SiteDescription, settings.LogoURL, settings.FaviconURL,
		settings.DefaultTheme, settings.PublicAddressMode, settings.ShowNetworkQuality,
		settings.ShowAccelerators, settings.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update site settings: %w", err)
	}
	if err := sessionRowsAffected(result, err, "update site settings"); err != nil {
		return err
	}
	return nil
}
