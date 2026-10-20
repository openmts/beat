package notification

import (
	"encoding/json"
	"strings"
)

type LegacyRevision struct {
	Canonical    CanonicalConfig
	TemplateText string
}

func ConvertLegacyChannel(kind, raw string, enabled bool) (LegacyRevision, error) {
	var config ProviderConfig
	var template string
	switch kind {
	case TypeWebhook:
		previous, err := parseWebhook(raw)
		if err != nil {
			return LegacyRevision{}, ErrProviderConfig
		}
		config.Endpoint, config.Headers, template = previous.URL, previous.Headers, previous.Template
	case TypeTelegram:
		previous, err := parseTelegram(raw)
		if err != nil {
			return LegacyRevision{}, ErrProviderConfig
		}
		config.BotToken, config.ChatID, config.ParseMode, template = previous.BotToken, previous.ChatID, previous.ParseMode, previous.Template
	case TypeEmail:
		previous, err := parseEmail(raw)
		if err != nil {
			return LegacyRevision{}, ErrProviderConfig
		}
		applyEmailDefaults(&previous)
		config.Host, config.Port, config.Username, config.Password = previous.Host, previous.Port, previous.Username, previous.Password
		config.From, config.To, config.Security, template = previous.From, previous.To, previous.Security, previous.Template
	default:
		return LegacyRevision{}, ErrProviderConfig
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return LegacyRevision{}, err
	}
	defer clear(encoded)
	canonical, err := CanonicalizeProvider(ConfigWrite{ChannelType: kind, Config: encoded, Enabled: enabled})
	if err != nil {
		return LegacyRevision{}, err
	}
	template = convertLegacyTemplate(template)
	if _, err := CompileTemplate(template, false); err != nil {
		clear(canonical.Secret)
		return LegacyRevision{}, err
	}
	return LegacyRevision{Canonical: canonical, TemplateText: template}, nil
}

func convertLegacyTemplate(value string) string {
	return strings.NewReplacer("{{rule}}", "{{rule.name}}", "{{node}}", "{{node.name}}", "{{metric}}", "{{metric.name}}",
		"{{severity}}", "{{event.severity}}", "{{value}}", "{{metric.value}}", "{{status}}", "{{event.status}}",
		"{{message}}", "{{event.message}}", "{{time}}", "{{event.occurred_at}}").Replace(value)
}
