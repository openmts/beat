package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"beat/internal/config"
	"beat/internal/platform/sqlite"
)

const systemSettingsKey = "system"

type SiteSettings struct {
	Name      string `json:"name"`
	Subtitle  string `json:"subtitle"`
	PublicURL string `json:"publicUrl"`
	Timezone  string `json:"timezone"`
}

type StorageSettings struct {
	MTSDataPath   string `json:"mtsDataPath"`
	RetentionDays int    `json:"retentionDays"`
	WALSync       bool   `json:"walSync"`
}

type SystemSettings struct {
	Site    SiteSettings    `json:"site"`
	Storage StorageSettings `json:"storage"`
}

type persistedSystemSettings struct {
	Settings  SystemSettings `json:"settings"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func defaultSystemSettings(cfg config.Config) SystemSettings {
	retentionDays := int(cfg.Retention.Hours() / 24)
	if retentionDays <= 0 {
		retentionDays = 7
	}
	return SystemSettings{
		Site: SiteSettings{
			Name:     "Beat",
			Subtitle: "NODE OBSERVABILITY",
			Timezone: "Asia/Shanghai",
		},
		Storage: StorageSettings{
			MTSDataPath:   cfg.MTSDataPath,
			RetentionDays: retentionDays,
			WALSync:       cfg.MTSSync,
		},
	}
}

func loadSystemSettings(ctx context.Context, store *sqlite.Store, defaults SystemSettings) (SystemSettings, time.Time, error) {
	raw, ok, err := store.GetSetting(ctx, systemSettingsKey)
	if err != nil {
		return defaults, time.Time{}, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return defaults, time.Time{}, nil
	}
	var persisted persistedSystemSettings
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return defaults, time.Time{}, fmt.Errorf("decode system settings: %w", err)
	}
	settings := normalizeSystemSettings(persisted.Settings, defaults)
	if err := validateSystemSettings(settings); err != nil {
		return defaults, time.Time{}, fmt.Errorf("validate system settings: %w", err)
	}
	return settings, persisted.UpdatedAt, nil
}

func normalizeSystemSettings(settings, defaults SystemSettings) SystemSettings {
	settings = cleanSystemSettings(settings)
	if strings.TrimSpace(settings.Site.Name) == "" {
		settings.Site.Name = defaults.Site.Name
	}
	if strings.TrimSpace(settings.Site.Subtitle) == "" {
		settings.Site.Subtitle = defaults.Site.Subtitle
	}
	if strings.TrimSpace(settings.Site.Timezone) == "" {
		settings.Site.Timezone = defaults.Site.Timezone
	}
	if strings.TrimSpace(settings.Storage.MTSDataPath) == "" {
		settings.Storage.MTSDataPath = defaults.Storage.MTSDataPath
	}
	if settings.Storage.RetentionDays == 0 {
		settings.Storage.RetentionDays = defaults.Storage.RetentionDays
	}
	return settings
}

func cleanSystemSettings(settings SystemSettings) SystemSettings {
	settings.Site.Name = strings.TrimSpace(settings.Site.Name)
	settings.Site.Subtitle = strings.TrimSpace(settings.Site.Subtitle)
	settings.Site.PublicURL = strings.TrimSpace(settings.Site.PublicURL)
	settings.Site.Timezone = strings.TrimSpace(settings.Site.Timezone)
	settings.Storage.MTSDataPath = strings.TrimSpace(settings.Storage.MTSDataPath)
	return settings
}

func validateSystemSettings(settings SystemSettings) error {
	if settings.Site.Name == "" || len([]rune(settings.Site.Name)) > 64 {
		return fmt.Errorf("站点名称不能为空且不能超过 64 个字符")
	}
	if len([]rune(settings.Site.Subtitle)) > 160 {
		return fmt.Errorf("站点副标题不能超过 160 个字符")
	}
	if settings.Site.PublicURL != "" {
		parsed, err := url.Parse(settings.Site.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("对外访问地址必须是有效的 HTTP 或 HTTPS 地址")
		}
	}
	if _, err := time.LoadLocation(settings.Site.Timezone); err != nil {
		return fmt.Errorf("默认时区无效")
	}
	if settings.Storage.MTSDataPath == "" || len([]rune(settings.Storage.MTSDataPath)) > 512 {
		return fmt.Errorf("MTS 数据目录不能为空且不能超过 512 个字符")
	}
	if settings.Storage.RetentionDays < 1 || settings.Storage.RetentionDays > 365 {
		return fmt.Errorf("MTS 数据保留天数必须在 1 到 365 之间")
	}
	return nil
}

func (s *Server) currentSettings() SystemSettings {
	settings, _ := s.settingsSnapshot()
	return settings
}

func (s *Server) settingsSnapshot() (SystemSettings, time.Time) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings, s.settingsUpdatedAt
}

func (s *Server) mtsRestartRequired() bool {
	settings := s.currentSettings()
	return settings.Storage.MTSDataPath != s.cfg.MTSDataPath ||
		settings.Storage.RetentionDays != int(s.cfg.Retention.Hours()/24) ||
		settings.Storage.WALSync != s.cfg.MTSSync
}

func (s *Server) configuredServerURL(r *http.Request) string {
	settings := s.currentSettings()
	if settings.Site.PublicURL != "" {
		return strings.TrimRight(settings.Site.PublicURL, "/")
	}
	return publicServerURL(r)
}
