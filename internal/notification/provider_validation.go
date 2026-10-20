package notification

import (
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

var secretNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

func validateProvider(kind string, c *ProviderConfig, enabled bool) error {
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = defaultDispatchTimeoutSeconds
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > maximumDispatchTimeoutSeconds || c.Redirects < 0 || c.Redirects > 3 {
		return ErrProviderConfig
	}
	if len(c.Headers) > maximumCollectionFields || len(c.Query) > maximumCollectionFields || len(c.NamedSecrets) > maximumNamedSecrets {
		return ErrProviderConfig
	}
	if err := validateHeaderTemplates(c.Headers); err != nil {
		return err
	}
	switch kind {
	case TypeWebhook:
		return validateEndpointConfig(c.Endpoint, enabled)
	case TypeTelegram:
		return validateTelegramProvider(c, enabled)
	case TypeEmail:
		return validateCommercialEmail(*c, enabled)
	case TypeBark:
		return validateBark(c, enabled)
	case TypeServerChanTurbo:
		return validateServerChanTurbo(c, enabled)
	case TypeServerChan3:
		return validateServerChan3(c, enabled)
	case TypeCustomHTTP:
		return validateCustomConfig(c, enabled)
	default:
		return ErrProviderConfig
	}
}

func validateEndpointConfig(value string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if !validProviderScalar(value, maximumEndpointBytes) {
		return ErrProviderConfig
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return ErrProviderConfig
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ErrProviderConfig
	}
	return nil
}

func validateOrigin(value string) error {
	if err := validateEndpointConfig(value, true); err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery {
		return ErrProviderConfig
	}
	return nil
}

func validateTelegramProvider(c *ProviderConfig, enabled bool) error {
	if c.Endpoint == "" {
		c.Endpoint = "https://api.telegram.org"
	}
	if enabled && (c.BotToken == "" || c.ChatID == "") {
		return ErrProviderConfig
	}
	if strings.ContainsAny(c.BotToken, "/\\?#% ") || !validProviderScalar(c.BotToken, 512) || !validProviderScalar(c.ChatID, 128) {
		return ErrProviderConfig
	}
	if c.ParseMode != "" && c.ParseMode != "HTML" && c.ParseMode != "MarkdownV2" {
		return ErrProviderConfig
	}
	return validateOrigin(c.Endpoint)
}

func validateBark(c *ProviderConfig, enabled bool) error {
	if c.Endpoint == "" {
		c.Endpoint = "https://api.day.app"
	}
	if c.Level == "" {
		c.Level = "active"
	}
	if c.Compatibility == "" {
		c.Compatibility = "strict"
	}
	if enabled && c.DeviceKey == "" {
		return ErrProviderConfig
	}
	if c.Level != "active" && c.Level != "timeSensitive" && c.Level != "passive" && c.Level != "critical" {
		return ErrProviderConfig
	}
	if c.Compatibility != "strict" && c.Compatibility != "http_2xx" {
		return ErrProviderConfig
	}
	if !validProviderScalar(c.DeviceKey, 512) || !validProviderScalar(c.Group, 128) || !validProviderScalar(c.Sound, 128) {
		return ErrProviderConfig
	}
	for _, raw := range []string{c.Icon, c.URL} {
		if raw == "" {
			continue
		}
		if err := validateEndpointConfig(raw, true); err != nil {
			return err
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" {
			return ErrProviderConfig
		}
	}
	return validateOrigin(c.Endpoint)
}

func validateServerChanTurbo(c *ProviderConfig, enabled bool) error {
	if c.Endpoint != "" && c.Endpoint != "https://sctapi.ftqq.com" {
		return ErrProviderConfig
	}
	c.Endpoint = "https://sctapi.ftqq.com"
	if enabled && c.SendKey == "" {
		return ErrProviderConfig
	}
	if strings.ContainsAny(c.SendKey, "/\\?#% ") || !validProviderScalar(c.SendKey, 512) || !validProviderScalar(c.OpenID, 1024) || !validProviderScalar(c.Channel, 256) {
		return ErrProviderConfig
	}
	return nil
}

func validateServerChan3(c *ProviderConfig, enabled bool) error {
	tags, seen := []string{}, map[string]bool{}
	for _, input := range c.Tags {
		if !validProviderScalar(input, 1024) {
			return ErrProviderConfig
		}
		for _, item := range strings.Split(input, "|") {
			tag := strings.TrimSpace(item)
			if len(tag) > 128 {
				return ErrProviderConfig
			}
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
	}
	if len(tags) > 8 {
		return ErrProviderConfig
	}
	c.Tags = tags
	return validateEndpointConfig(c.Endpoint, enabled)
}

func validateCommercialEmail(c ProviderConfig, enabled bool) error {
	if enabled && (c.Host == "" || c.Port == 0 || c.From == "" || len(c.To) == 0 || c.Security == "") {
		return ErrProviderConfig
	}
	if c.Host != "" {
		if _, err := canonicalHost(c.Host); err != nil {
			return ErrProviderConfig
		}
	}
	if c.Port < 0 || c.Port > 65535 || len(c.To) > maximumRecipients {
		return ErrProviderConfig
	}
	if c.Security != "" && c.Security != "starttls" && c.Security != "tls" && c.Security != "none" {
		return ErrProviderConfig
	}
	if !validProviderScalar(c.Username, maximumHeaderBytes) || !validProviderScalar(c.Password, maximumHeaderBytes) {
		return ErrProviderConfig
	}
	if c.Security == "none" {
		ip := net.ParseIP(c.Host)
		if c.Host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return ErrProviderConfig
		}
	}
	addresses := append([]string{}, c.To...)
	if c.From != "" {
		addresses = append(addresses, c.From)
	}
	for _, address := range addresses {
		if _, err := mail.ParseAddress(address); err != nil || !validProviderScalar(address, 512) {
			return ErrProviderConfig
		}
	}
	return nil
}
