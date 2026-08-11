package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeConfig(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		raw         string
		existing    string
		wantPart    string
		wantError   bool
	}{
		{name: "legacy webhook", channelType: TypeWebhook, raw: "https://example.com/hook", wantPart: `"url":"https://example.com/hook"`},
		{name: "telegram", channelType: TypeTelegram, raw: `{"bot_token":"token","chat_id":"42"}`, wantPart: `"chat_id":"42"`},
		{name: "telegram keeps secret", channelType: TypeTelegram, raw: `{"bot_token":"","chat_id":"84"}`, existing: `{"bot_token":"old","chat_id":"42"}`, wantPart: `"bot_token":"old"`},
		{name: "email", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"username":"beat","password":"secret","from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`, wantPart: `"security":"starttls"`},
		{name: "email keeps password", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":465,"username":"beat","password":"","from":"beat@example.com","to":["ops@example.com"],"security":"tls"}`, existing: `{"host":"smtp.example.com","port":465,"username":"beat","password":"old","from":"beat@example.com","to":["ops@example.com"],"security":"tls"}`, wantPart: `"password":"old"`},
		{name: "unknown type", channelType: "sms", raw: `{}`, wantError: true},
		{name: "invalid webhook", channelType: TypeWebhook, raw: "ftp://example.com", wantError: true},
		{name: "missing telegram token", channelType: TypeTelegram, raw: `{"chat_id":"42"}`, wantError: true},
		{name: "insecure remote smtp", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":25,"from":"beat@example.com","to":["ops@example.com"],"security":"none"}`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeConfig(tt.channelType, tt.raw, tt.existing)
			if tt.wantError {
				if err == nil {
					t.Fatalf("NormalizeConfig() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeConfig() error = %v", err)
			}
			if !strings.Contains(got, tt.wantPart) {
				t.Fatalf("NormalizeConfig() = %q, want part %q", got, tt.wantPart)
			}
		})
	}
}

func TestSanitizeConfig(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		raw         string
		secret      string
		flag        string
	}{
		{name: "telegram", channelType: TypeTelegram, raw: `{"bot_token":"secret-token","chat_id":"42"}`, secret: "secret-token", flag: "has_bot_token"},
		{name: "email", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"username":"beat","password":"secret-password","from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`, secret: "secret-password", flag: "has_password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeConfig(tt.channelType, tt.raw)
			if strings.Contains(got, tt.secret) || !strings.Contains(got, tt.flag) {
				t.Fatalf("SanitizeConfig() = %q", got)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(got), &payload); err != nil {
				t.Fatalf("sanitized config is not JSON: %v", err)
			}
		})
	}
}

func TestConfigValidationEdges(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		raw         string
		existing    string
	}{
		{name: "malformed webhook", channelType: TypeWebhook, raw: "{"},
		{name: "webhook without host", channelType: TypeWebhook, raw: `{"url":"https:///hook"}`},
		{name: "malformed telegram", channelType: TypeTelegram, raw: "{"},
		{name: "malformed previous telegram", channelType: TypeTelegram, raw: `{"chat_id":"42"}`, existing: "{"},
		{name: "invalid telegram token", channelType: TypeTelegram, raw: `{"bot_token":"bad token","chat_id":"42"}`},
		{name: "malformed email", channelType: TypeEmail, raw: "{"},
		{name: "malformed previous email", channelType: TypeEmail, raw: `{"host":"localhost","port":25,"username":"beat","from":"beat@example.com","to":["ops@example.com"],"security":"none"}`, existing: "{"},
		{name: "invalid SMTP security", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["ops@example.com"],"security":"ssl3"}`},
		{name: "missing SMTP password", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"username":"beat","from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`},
		{name: "invalid sender", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"from":"bad","to":["ops@example.com"],"security":"starttls"}`},
		{name: "missing recipients", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":[],"security":"starttls"}`},
		{name: "invalid recipient", channelType: TypeEmail, raw: `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["bad"],"security":"starttls"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeConfig(test.channelType, test.raw, test.existing); err == nil {
				t.Fatal("NormalizeConfig() error = nil")
			}
		})
	}

	for name, raw := range map[string]string{
		"default starttls port": `{"host":"smtp.example.com","from":"beat@example.com","to":["ops@example.com"]}`,
		"default TLS port":      `{"host":"smtp.example.com","from":"beat@example.com","to":["ops@example.com"],"security":"tls"}`,
		"loopback plaintext":    `{"host":"localhost","port":25,"from":"beat@example.com","to":["ops@example.com"],"security":"none"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeConfig(TypeEmail, raw, ""); err != nil {
				t.Fatalf("NormalizeConfig() error = %v", err)
			}
		})
	}
	canonical, err := NormalizeConfig(TypeEmail, `{"host":"smtp.example.com","port":587,"from":"Beat <beat@example.com>","to":["Ops <ops@example.com>"],"security":"starttls"}`, "")
	if err != nil || strings.Contains(canonical, "Beat <") || !strings.Contains(canonical, `"from":"beat@example.com"`) {
		t.Fatalf("canonical email config = %q, error = %v", canonical, err)
	}
}

func TestSanitizeConfigFallbacks(t *testing.T) {
	if got := SanitizeConfig(TypeWebhook, "{"); got != "{}" {
		t.Fatalf("invalid config = %q", got)
	}
	if got := SanitizeConfig("sms", `{}`); got != "{}" {
		t.Fatalf("unknown config = %q", got)
	}
	if got := sanitizedJSON(make(chan int), nil); got != "{}" {
		t.Fatalf("unencodable config = %q", got)
	}
	if !isLoopbackHost("127.0.0.1") || isLoopbackHost("example.com") {
		t.Fatal("loopback host detection is incorrect")
	}
}
