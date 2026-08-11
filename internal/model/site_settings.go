package model

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

type SiteSettings struct {
	SiteTitle          string    `json:"site_title"`
	SiteDescription    string    `json:"site_description"`
	LogoURL            string    `json:"logo_url"`
	FaviconURL         string    `json:"favicon_url"`
	DefaultTheme       string    `json:"default_theme"`
	ShowIPAddresses    bool      `json:"show_ip_addresses"`
	ShowNetworkQuality bool      `json:"show_network_quality"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func DefaultSiteSettings() SiteSettings {
	return SiteSettings{
		SiteTitle:          "Beat Monitor",
		SiteDescription:    "Server monitoring and operations dashboard.",
		FaviconURL:         "/favicon.svg",
		DefaultTheme:       ThemeSystem,
		ShowIPAddresses:    true,
		ShowNetworkQuality: true,
	}
}

func (settings *SiteSettings) Normalize() {
	settings.SiteTitle = strings.TrimSpace(settings.SiteTitle)
	settings.SiteDescription = strings.TrimSpace(settings.SiteDescription)
	settings.LogoURL = strings.TrimSpace(settings.LogoURL)
	settings.FaviconURL = strings.TrimSpace(settings.FaviconURL)
	settings.DefaultTheme = strings.TrimSpace(settings.DefaultTheme)
}

func (settings SiteSettings) Validate() error {
	if settings.SiteTitle == "" || utf8.RuneCountInString(settings.SiteTitle) > 80 {
		return errors.New("site title must contain 1 to 80 characters")
	}
	if utf8.RuneCountInString(settings.SiteDescription) > 240 {
		return errors.New("site description must not exceed 240 characters")
	}
	if !validTheme(settings.DefaultTheme) {
		return errors.New("default theme is invalid")
	}
	if !validAssetURL(settings.LogoURL) || !validAssetURL(settings.FaviconURL) {
		return errors.New("branding URL is invalid")
	}
	return nil
}

func validTheme(theme string) bool {
	return theme == ThemeSystem || theme == ThemeLight || theme == ThemeDark
}

func validAssetURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.ContainsAny(value, "\\#") || strings.HasPrefix(value, "//") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return parsed.Host == ""
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
