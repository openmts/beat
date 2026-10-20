package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSiteSettingsStore(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewSiteSettingsStore(sqliteStore.DB)
	settings, err := settingsStore.Get(t.Context())
	if err != nil {
		t.Fatalf("get defaults: %v", err)
	}
	if settings.SiteTitle != "Beat Monitor" || settings.FaviconURL != "/favicon.svg" {
		t.Fatalf("defaults = %#v", settings)
	}
	if settings.PublicAddressMode != model.PublicAddressHidden {
		t.Fatalf("a fresh instance must default to hidden addresses, got %q", settings.PublicAddressMode)
	}

	settings.SiteTitle = "Operations"
	settings.SiteDescription = "  Infrastructure status  "
	settings.LogoURL = "https://example.com/logo.svg"
	settings.DefaultTheme = model.ThemeDark
	settings.PublicAddressMode = model.PublicAddressMasked
	settings.ShowNetworkQuality = false
	settings.ShowAccelerators = false
	updated, err := settingsStore.Update(t.Context(), settings)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.SiteDescription != "Infrastructure status" || updated.UpdatedAt.IsZero() {
		t.Fatalf("updated = %#v", updated)
	}
	persisted, err := settingsStore.Get(t.Context())
	if err != nil {
		t.Fatalf("get updated settings: %v", err)
	}
	if persisted.SiteTitle != "Operations" || persisted.PublicAddressMode != model.PublicAddressMasked ||
		persisted.ShowNetworkQuality || persisted.ShowAccelerators || persisted.DefaultTheme != model.ThemeDark {
		t.Fatalf("persisted = %#v", persisted)
	}
}

func TestSiteSettingsStoreErrors(t *testing.T) {
	sqliteStore := setupTestDB(t)
	settingsStore := NewSiteSettingsStore(sqliteStore.DB)
	invalid := model.DefaultSiteSettings()
	invalid.SiteTitle = ""
	if _, err := settingsStore.Update(t.Context(), invalid); err == nil {
		t.Fatal("expected validation error")
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := settingsStore.Get(t.Context()); err == nil {
		t.Fatal("expected get error")
	}
	if _, err := settingsStore.Update(t.Context(), model.DefaultSiteSettings()); err == nil {
		t.Fatal("expected update error")
	}
}
