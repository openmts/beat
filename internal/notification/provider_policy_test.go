package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderRejectsFieldsFromOtherProviders(t *testing.T) {
	for _, kind := range []string{TypeWebhook, TypeTelegram, TypeEmail, TypeBark, TypeServerChanTurbo, TypeServerChan3} {
		t.Run(kind, func(t *testing.T) {
			_, err := CanonicalizeProvider(ConfigWrite{ChannelType: kind, Config: json.RawMessage(`{"body":"unreviewed"}`)})
			if err == nil {
				t.Fatal("provider accepted a field outside its configuration contract")
			}
		})
	}
}

func TestCustomConfigurationRejectsInvalidRequestBeforeSaving(t *testing.T) {
	cases := []string{
		`"body":{"value":"{{unknown}}"}`,
		`"body":{"value":"{{secret.missing}}"}`,
		`"body":{"{{text}}":"value"}`,
		`"body":{"key}}":"value"}`,
		`"body_mode":"text","body":{}`,
		`"body_mode":"form","body":{"number":1}`,
		`"method":"GET","body":"body"`,
		`"query":{"value":"{{secret.missing}}"}`,
		`"query":{"{{text}}":"value"}`,
		`"headers":{"X-Token":"{{secret.missing}}"}`,
		`"headers":{"X-Token":"a","x-token":"b"}`,
		`"auth":{"mode":"bearer","token":"a\r\nb"}`,
		`"auth":{"mode":"bearer"}`,
		`"auth":{"mode":"basic","username":"user:name","password":"password"}`,
		`"auth":{"mode":"none","token":"ignored-secret"}`,
		`"auth":{"mode":"basic","username":"name","password":"password","token":"ignored"}`,
		`"hmac":{"secret":"key","header":"X-Signature","timestamp_header":"x-signature"}`,
		`"hmac":{"secret":"key","header":"Authorization"}`,
		`"hmac":{"secret":"key"},"headers":{"X-Beat-Signature":"overwritten"}`,
	}
	for _, fields := range cases {
		t.Run(fields, func(t *testing.T) {
			config := json.RawMessage(`{"endpoint":"https://receiver.example",` + fields + `}`)
			if _, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeCustomHTTP, Config: config, Enabled: true}); err == nil {
				t.Fatal("invalid request was accepted for storage")
			}
		})
	}
}

func TestSecretNullAndEmptyValuesPreservePreviousConfiguration(t *testing.T) {
	first, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeEmail, Config: json.RawMessage(`{"host":"smtp.example","port":465,"from":"a@example.test","to":["b@example.test"],"security":"tls"}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeEmail, Previous: first.Secret, Config: json.RawMessage(`{"host":null,"from":"","to":[]}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeProvider(updated.Secret)
	if err != nil || config.Host != "smtp.example" || config.From != "a@example.test" || len(config.To) != 1 {
		t.Fatal("blank input removed a stored secret without an explicit clear")
	}
}

func TestProviderCorruptPreviousAndInvalidClearFailClosed(t *testing.T) {
	for _, previous := range []string{"null", `[]`, `{`, strings.Repeat(" ", MaximumConfigBytes+1)} {
		_, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeWebhook, Previous: json.RawMessage(previous), Config: json.RawMessage(`{"endpoint":"https://receiver.example"}`)})
		if err == nil {
			t.Fatal("corrupt previous configuration accepted")
		}
	}
	for _, path := range []string{"timeout_seconds", "named_secrets.bad.name", "auth.token"} {
		_, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeWebhook, Config: json.RawMessage(`{}`), ClearSecrets: []string{path}})
		if err == nil {
			t.Fatal("invalid provider clear path accepted")
		}
	}
}

func TestServerChanTagsAreCanonicalAndBounded(t *testing.T) {
	canonical, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeServerChan3, Config: json.RawMessage(`{"endpoint":"https://receiver.example","tags":[" first | second ","first","", "third"]}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeProvider(canonical.Secret)
	if err != nil || strings.Join(config.Tags, "|") != "first|second|third" {
		t.Fatal("tags were not split, trimmed and deduplicated")
	}
}
