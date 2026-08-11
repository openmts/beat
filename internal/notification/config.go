package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
)

const (
	TypeWebhook  = "webhook"
	TypeTelegram = "telegram"
	TypeEmail    = "email"

	EmailSecuritySTARTTLS = "starttls"
	EmailSecurityTLS      = "tls"
	EmailSecurityNone     = "none"
)

type WebhookConfig struct {
	URL string `json:"url"`
}

type TelegramConfig struct {
	BotToken    string `json:"bot_token"`
	ChatID      string `json:"chat_id"`
	HasBotToken bool   `json:"has_bot_token,omitempty"`
}

type EmailConfig struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username,omitempty"`
	Password    string   `json:"password,omitempty"`
	From        string   `json:"from"`
	To          []string `json:"to"`
	Security    string   `json:"security"`
	HasPassword bool     `json:"has_password,omitempty"`
}

func NormalizeConfig(channelType, raw, existing string) (string, error) {
	switch channelType {
	case TypeWebhook:
		return normalizeWebhook(raw)
	case TypeTelegram:
		return normalizeTelegram(raw, existing)
	case TypeEmail:
		return normalizeEmail(raw, existing)
	default:
		return "", fmt.Errorf("unsupported channel type %q", channelType)
	}
}

func SanitizeConfig(channelType, raw string) string {
	switch channelType {
	case TypeWebhook:
		config, err := parseWebhook(raw)
		return sanitizedJSON(config, err)
	case TypeTelegram:
		config, err := parseTelegram(raw)
		config.HasBotToken = config.BotToken != ""
		config.BotToken = ""
		return sanitizedJSON(config, err)
	case TypeEmail:
		config, err := parseEmail(raw)
		config.HasPassword = config.Password != ""
		config.Password = ""
		return sanitizedJSON(config, err)
	default:
		return "{}"
	}
}

func normalizeWebhook(raw string) (string, error) {
	config, err := parseWebhook(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("webhook URL must use HTTP or HTTPS")
	}
	return marshalConfig(config)
}

func parseWebhook(raw string) (WebhookConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return WebhookConfig{URL: trimmed}, nil
	}
	var config WebhookConfig
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return config, fmt.Errorf("decode webhook config: %w", err)
	}
	config.URL = strings.TrimSpace(config.URL)
	return config, nil
}

func normalizeTelegram(raw, existing string) (string, error) {
	config, err := parseTelegram(raw)
	if err != nil {
		return "", err
	}
	if config.BotToken == "" && existing != "" {
		previous, previousErr := parseTelegram(existing)
		if previousErr != nil {
			return "", previousErr
		}
		config.BotToken = previous.BotToken
	}
	if config.BotToken == "" || config.ChatID == "" {
		return "", errors.New("telegram bot token and chat ID are required")
	}
	if strings.ContainsAny(config.BotToken, "/\\\r\n\t ") {
		return "", errors.New("telegram bot token is invalid")
	}
	config.HasBotToken = false
	return marshalConfig(config)
}

func parseTelegram(raw string) (TelegramConfig, error) {
	var config TelegramConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, fmt.Errorf("decode Telegram config: %w", err)
	}
	config.BotToken = strings.TrimSpace(config.BotToken)
	config.ChatID = strings.TrimSpace(config.ChatID)
	return config, nil
}

func normalizeEmail(raw, existing string) (string, error) {
	config, err := parseEmail(raw)
	if err != nil {
		return "", err
	}
	if config.Password == "" && existing != "" {
		previous, previousErr := parseEmail(existing)
		if previousErr != nil {
			return "", previousErr
		}
		config.Password = previous.Password
	}
	applyEmailDefaults(&config)
	if err := validateEmail(config); err != nil {
		return "", err
	}
	canonicalizeEmailAddresses(&config)
	config.HasPassword = false
	return marshalConfig(config)
}

func parseEmail(raw string) (EmailConfig, error) {
	var config EmailConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, fmt.Errorf("decode email config: %w", err)
	}
	config.Host = strings.TrimSpace(config.Host)
	config.Username = strings.TrimSpace(config.Username)
	config.From = strings.TrimSpace(config.From)
	config.Security = strings.TrimSpace(config.Security)
	for index := range config.To {
		config.To[index] = strings.TrimSpace(config.To[index])
	}
	return config, nil
}

func applyEmailDefaults(config *EmailConfig) {
	if config.Security == "" {
		config.Security = EmailSecuritySTARTTLS
	}
	if config.Port == 0 && config.Security == EmailSecurityTLS {
		config.Port = 465
	}
	if config.Port == 0 {
		config.Port = 587
	}
}

func validateEmail(config EmailConfig) error {
	if config.Host == "" || config.Port < 1 || config.Port > 65535 {
		return errors.New("SMTP host and valid port are required")
	}
	if config.Security != EmailSecuritySTARTTLS && config.Security != EmailSecurityTLS && config.Security != EmailSecurityNone {
		return errors.New("SMTP security must be starttls, tls or none")
	}
	if config.Security == EmailSecurityNone && !isLoopbackHost(config.Host) {
		return errors.New("unencrypted SMTP is only allowed on loopback hosts")
	}
	if config.Username != "" && config.Password == "" {
		return errors.New("SMTP password is required when username is set")
	}
	if _, err := mail.ParseAddress(config.From); err != nil {
		return errors.New("valid sender address is required")
	}
	if len(config.To) == 0 {
		return errors.New("at least one recipient is required")
	}
	for _, recipient := range config.To {
		if _, err := mail.ParseAddress(recipient); err != nil {
			return errors.New("all recipient addresses must be valid")
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func canonicalizeEmailAddresses(config *EmailConfig) {
	from, _ := mail.ParseAddress(config.From)
	config.From = from.Address
	for index, raw := range config.To {
		recipient, _ := mail.ParseAddress(raw)
		config.To[index] = recipient.Address
	}
}

func marshalConfig(config any) (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode channel config: %w", err)
	}
	return string(payload), nil
}

func sanitizedJSON(config any, parseErr error) string {
	if parseErr != nil {
		return "{}"
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(payload)
}
