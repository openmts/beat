package notification

import (
	"encoding/json"
	"strings"

	"github.com/beat/backend/internal/model"
)

func validateCustomConfig(c *ProviderConfig, enabled bool) error {
	if c.Method == "" {
		c.Method = "POST"
	}
	if c.BodyMode == "" {
		c.BodyMode = "json"
	}
	if c.Method != "GET" && c.Method != "POST" || c.BodyMode != "json" && c.BodyMode != "form" && c.BodyMode != "text" {
		return ErrProviderConfig
	}
	if strings.Contains(c.Endpoint, "{{") || strings.Contains(c.Endpoint, "}}") {
		return ErrProviderConfig
	}
	if err := validateEndpointConfig(c.Endpoint, enabled); err != nil {
		return err
	}
	for name, value := range c.NamedSecrets {
		if !secretNamePattern.MatchString(name) || !validProviderScalar(value, maximumHeaderBytes) {
			return ErrProviderConfig
		}
	}
	if err := validateHTTPAuth(c.Auth, enabled); err != nil {
		return err
	}
	if err := validateSignature(c, enabled); err != nil {
		return err
	}
	if err := validateCustomFields(*c); err != nil {
		return err
	}
	_, _, _, err := customRequest(*c, model.NotificationPayload{})
	return err
}

func validateCustomFields(config ProviderConfig) error {
	if len(config.Body) > 0 && !json.Valid(config.Body) {
		return ErrProviderConfig
	}
	for key, value := range config.Query {
		if key == "" || !validProviderScalar(key, 128) || strings.Contains(key, "{{") || strings.Contains(key, "}}") {
			return ErrProviderConfig
		}
		if _, err := renderCustomValue(value, TemplateValues{Secrets: config.NamedSecrets, Maximum: maximumEndpointBytes}); err != nil {
			return err
		}
	}
	for key, value := range config.Headers {
		if strings.EqualFold(key, "Authorization") && config.Auth != nil && config.Auth.Mode != "none" {
			return ErrProviderConfig
		}
		if _, err := renderCustomValue(value, TemplateValues{Secrets: config.NamedSecrets, Maximum: maximumHeaderBytes}); err != nil {
			return err
		}
	}
	return nil
}
