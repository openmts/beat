package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyConversionPreservesProvidersAndRewritesSupportedTemplates(t *testing.T) {
	cases := []struct{ kind, raw string }{
		{TypeWebhook, `{"url":"https://receiver.example/PRIVATE_PATH","headers":{"Authorization":"PRIVATE_KEY"},"template":"{{rule}} {{node}} {{value}} {{time}}"}`},
		{TypeTelegram, `{"bot_token":"PRIVATE_BOT","chat_id":"PRIVATE_CHAT","template":"{{message}} {{status}}"}`},
		{TypeEmail, `{"host":"smtp.example","from":"sender@example.test","to":["recipient@example.test"],"template":"{{severity}} {{metric}}"}`},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			converted, err := ConvertLegacyChannel(test.kind, test.raw, true)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(converted.Canonical.Public), "PRIVATE_") || strings.Contains(converted.TemplateText, "{{rule}}") {
				t.Fatal("legacy conversion leaked credentials or kept obsolete template aliases")
			}
			if !json.Valid(converted.Canonical.Secret) {
				t.Fatal("legacy provider document was not canonical JSON")
			}
		})
	}
}

func TestLegacyConversionRejectsMalformedConfigAndUnknownTemplates(t *testing.T) {
	for _, test := range []struct{ kind, raw string }{
		{TypeWebhook, `{"url":false}`}, {TypeTelegram, `{"chat_id":false}`}, {TypeEmail, `{"port":"wrong"}`},
		{TypeCustomHTTP, `{}`}, {TypeWebhook, `{"url":"file:///etc/passwd"}`},
		{TypeWebhook, `{"url":"https://receiver.example","template":"{{env.HOME}}"}`},
	} {
		if _, err := ConvertLegacyChannel(test.kind, test.raw, true); err == nil {
			t.Fatal("invalid legacy configuration was silently migrated")
		}
	}
}
