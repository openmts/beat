package store

import (
	"context"
	"database/sql"
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
	var settings model.SiteSettings
	var showIP, showNetwork int
	err := store.db.QueryRowContext(ctx, `SELECT site_title, site_description, logo_url,
		favicon_url, default_theme, show_ip_addresses, show_network_quality, updated_at
		FROM site_settings WHERE id = 1`).Scan(
		&settings.SiteTitle, &settings.SiteDescription, &settings.LogoURL,
		&settings.FaviconURL, &settings.DefaultTheme, &showIP, &showNetwork, &settings.UpdatedAt,
	)
	if err != nil {
		return model.SiteSettings{}, fmt.Errorf("get site settings: %w", err)
	}
	settings.ShowIPAddresses = showIP == 1
	settings.ShowNetworkQuality = showNetwork == 1
	return settings, nil
}

func (store *SiteSettingsStore) Update(
	ctx context.Context,
	settings model.SiteSettings,
) (model.SiteSettings, error) {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return model.SiteSettings{}, err
	}
	settings.UpdatedAt = model.NowUTC()
	_, err := store.db.ExecContext(ctx, `UPDATE site_settings SET site_title = ?,
		site_description = ?, logo_url = ?, favicon_url = ?, default_theme = ?,
		show_ip_addresses = ?, show_network_quality = ?, updated_at = ? WHERE id = 1`,
		settings.SiteTitle, settings.SiteDescription, settings.LogoURL, settings.FaviconURL,
		settings.DefaultTheme, settings.ShowIPAddresses, settings.ShowNetworkQuality, settings.UpdatedAt,
	)
	if err != nil {
		return model.SiteSettings{}, fmt.Errorf("update site settings: %w", err)
	}
	return settings, nil
}
