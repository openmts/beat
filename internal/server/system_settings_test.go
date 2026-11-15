package server

import (
	"testing"
	"time"

	"beat/internal/config"
)

func TestDefaultSystemSettings(t *testing.T) {
	cfg := config.Config{MTSDataPath: "/var/lib/beat/mts", Retention: 7 * 24 * time.Hour}
	settings := defaultSystemSettings(cfg)
	if settings.Site.Name != "Beat" || settings.Site.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected site defaults: %+v", settings.Site)
	}
	if settings.Storage.MTSDataPath != cfg.MTSDataPath || settings.Storage.RetentionDays != 7 {
		t.Fatalf("unexpected storage defaults: %+v", settings.Storage)
	}
}

func TestValidateSystemSettings(t *testing.T) {
	valid := SystemSettings{
		Site:    SiteSettings{Name: "NOC", Subtitle: "Node observability", PublicURL: "https://beat.example.com", Timezone: "Asia/Shanghai"},
		Storage: StorageSettings{MTSDataPath: "/var/lib/beat/mts", RetentionDays: 30},
	}
	if err := validateSystemSettings(valid); err != nil {
		t.Fatalf("validateSystemSettings() valid error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*SystemSettings)
	}{
		{name: "empty name", mutate: func(settings *SystemSettings) { settings.Site.Name = "" }},
		{name: "invalid url", mutate: func(settings *SystemSettings) { settings.Site.PublicURL = "ftp://example.com" }},
		{name: "invalid timezone", mutate: func(settings *SystemSettings) { settings.Site.Timezone = "Mars/Olympus" }},
		{name: "empty mts path", mutate: func(settings *SystemSettings) { settings.Storage.MTSDataPath = "" }},
		{name: "retention too small", mutate: func(settings *SystemSettings) { settings.Storage.RetentionDays = 0 }},
		{name: "retention too large", mutate: func(settings *SystemSettings) { settings.Storage.RetentionDays = 366 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := valid
			test.mutate(&settings)
			if err := validateSystemSettings(settings); err == nil {
				t.Fatal("validateSystemSettings() error = nil")
			}
		})
	}
}
