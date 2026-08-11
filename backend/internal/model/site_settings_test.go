package model

import (
	"strings"
	"testing"
)

func TestSiteSettingsDefaultsAndNormalization(t *testing.T) {
	settings := DefaultSiteSettings()
	if settings.SiteTitle != "Beat Monitor" || settings.DefaultTheme != ThemeSystem ||
		!settings.ShowIPAddresses || !settings.ShowNetworkQuality {
		t.Fatalf("defaults = %#v", settings)
	}
	settings.SiteTitle = "  Status  "
	settings.LogoURL = "  /logo.svg  "
	settings.Normalize()
	if settings.SiteTitle != "Status" || settings.LogoURL != "/logo.svg" {
		t.Fatalf("normalized = %#v", settings)
	}
	if err := settings.Validate(); err != nil {
		t.Fatalf("validate defaults: %v", err)
	}
}

func TestSiteSettingsValidation(t *testing.T) {
	valid := DefaultSiteSettings()
	tests := []struct {
		name   string
		mutate func(*SiteSettings)
	}{
		{name: "empty title", mutate: func(value *SiteSettings) { value.SiteTitle = "" }},
		{name: "long title", mutate: func(value *SiteSettings) { value.SiteTitle = strings.Repeat("x", 81) }},
		{name: "long description", mutate: func(value *SiteSettings) { value.SiteDescription = strings.Repeat("x", 241) }},
		{name: "invalid theme", mutate: func(value *SiteSettings) { value.DefaultTheme = "purple" }},
		{name: "script URL", mutate: func(value *SiteSettings) { value.LogoURL = "javascript:alert(1)" }},
		{name: "protocol relative URL", mutate: func(value *SiteSettings) { value.LogoURL = "//example.com/a.svg" }},
		{name: "URL credentials", mutate: func(value *SiteSettings) { value.LogoURL = "https://user@example.com/a.svg" }},
		{name: "URL fragment", mutate: func(value *SiteSettings) { value.FaviconURL = "/favicon.svg#icon" }},
		{name: "backslash", mutate: func(value *SiteSettings) { value.FaviconURL = `/bad\icon.svg` }},
		{name: "long URL", mutate: func(value *SiteSettings) { value.LogoURL = "https://example.com/" + strings.Repeat("x", 2048) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := valid
			test.mutate(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatalf("expected validation error for %#v", settings)
			}
		})
	}
	for _, assetURL := range []string{"", "/brand/logo.svg", "https://example.com/logo.svg", "http://example.com/icon.ico"} {
		settings := valid
		settings.LogoURL = assetURL
		if err := settings.Validate(); err != nil {
			t.Fatalf("valid URL %q: %v", assetURL, err)
		}
	}
}
