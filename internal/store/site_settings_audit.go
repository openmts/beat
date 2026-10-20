package store

import "github.com/beat/backend/internal/model"

func siteSettingsEvidence(before, after model.SiteSettings) metadataEvidence {
	fields := []string{}
	changes := []struct {
		name    string
		changed bool
	}{
		{"site_title", before.SiteTitle != after.SiteTitle}, {"site_description", before.SiteDescription != after.SiteDescription},
		{"logo_url", before.LogoURL != after.LogoURL}, {"favicon_url", before.FaviconURL != after.FaviconURL},
		{"default_theme", before.DefaultTheme != after.DefaultTheme}, {"public_address_mode", before.PublicAddressMode != after.PublicAddressMode},
		{"show_network_quality", before.ShowNetworkQuality != after.ShowNetworkQuality},
		{"show_accelerators", before.ShowAccelerators != after.ShowAccelerators},
	}
	for _, change := range changes {
		if change.changed {
			fields = append(fields, change.name)
		}
	}
	return metadataEvidence{Action: "site_settings.update", Type: "site_settings", ID: "global", Fields: fields}
}
