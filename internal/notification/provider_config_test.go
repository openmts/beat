package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestProviderConfigsKeepSecretsPrivateAndReplaceExplicitMaps(t *testing.T) {
	first, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeWebhook, Config: json.RawMessage(`{"endpoint":"https://PRIVATE_HOST.example/PRIVATE_PATH","headers":{"Old":"PRIVATE_OLD"}}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeWebhook, Previous: first.Secret, Config: json.RawMessage(`{"endpoint":"","headers":{"New":"PRIVATE_NEW"}}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeProvider(updated.Secret)
	if err != nil || len(config.Headers) != 1 || config.Headers["New"] != "PRIVATE_NEW" || config.Endpoint == "" {
		t.Fatal("map replacement/secret preservation failed")
	}
	cleared, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeWebhook, Previous: updated.Secret, Config: json.RawMessage(`{}`), ClearSecrets: []string{"headers", "endpoint"}})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Present["headers"] || cleared.Present["endpoint"] {
		t.Fatal("explicit clear retained secret")
	}
	if strings.Contains(string(updated.Public), "PRIVATE_") || fmt.Sprintf("%#v", config) != "notification provider configuration" {
		t.Fatal("configuration exposed")
	}
}

func testDispatch(kind string, config json.RawMessage) model.NotificationDispatch {
	return model.NotificationDispatch{DeliveryID: "preview", ChannelType: kind, Attempt: 1, Config: config, Payload: model.NotificationPayload{Subject: "Subject", Text: "Message", Data: map[string]string{"event.kind": "notification.test"}}}
}

func TestEveryProviderConfigAndPreviewRedaction(t *testing.T) {
	cases := []struct{ kind, raw string }{
		{TypeTelegram, `{"bot_token":"PRIVATE_TOKEN","chat_id":"PRIVATE_CHAT","parse_mode":"MarkdownV2"}`},
		{TypeEmail, `{"host":"smtp.example","port":465,"username":"PRIVATE_USER","password":"PRIVATE_PASSWORD","from":"sender@example.test","to":["recipient@example.test"],"security":"tls"}`},
		{TypeBark, `{"device_key":"PRIVATE_DEVICE","level":"critical","group":"group","sound":"sound","icon":"https://icons.example/private","url":"https://click.example/private"}`},
		{TypeServerChanTurbo, `{"send_key":"PRIVATE_KEY","open_id":"PRIVATE_OPENID","noip":true}`},
		{TypeServerChan3, `{"endpoint":"https://serverchan.example/PRIVATE_PATH","tags":["tag"]}`},
		{TypeCustomHTTP, `{"endpoint":"https://receiver.example/PRIVATE_PATH","method":"POST","body_mode":"text","body":"{{secret.token}} {{text}}","named_secrets":{"token":"PRIVATE_TOKEN"},"auth":{"mode":"basic","username":"PRIVATE_USERNAME","password":"PRIVATE_PASSWORD"},"hmac":{"secret":"PRIVATE_HMAC"}}`},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			config, err := CanonicalizeProvider(ConfigWrite{ChannelType: test.kind, Config: json.RawMessage(test.raw), Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(config.Public), "PRIVATE_") {
				t.Fatal("public config leaked secret")
			}
			preview, err := PreviewDispatch(testDispatch(test.kind, config.Secret))
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(preview)
			if err != nil || strings.Contains(string(encoded), "PRIVATE_") {
				t.Fatalf("preview leak: %s", encoded)
			}
		})
	}
}

func TestProviderValidationRejectsInvalidTemplatesAndFields(t *testing.T) {
	cases := []struct{ kind, raw string }{
		{TypeWebhook, `{"endpoint":"file:///etc/passwd"}`}, {TypeWebhook, `{"endpoint":"https://user:password@host.test"}`},
		{TypeWebhook, `{"endpoint":"https://host.test","headers":{"Host":"elsewhere"}}`}, {TypeWebhook, `{"endpoint":"https://host.test","headers":{"X-Test":"a\r\nb"}}`},
		{TypeTelegram, `{"bot_token":"token/invalid","chat_id":"chat"}`}, {TypeTelegram, `{"bot_token":"token","chat_id":"chat","parse_mode":"javascript"}`},
		{TypeEmail, `{"host":"smtp.example","port":25,"from":"a@example.test","to":["b@example.test"],"security":"none"}`},
		{TypeEmail, `{"host":"smtp.example","port":465,"from":"bad","to":["b@example.test"],"security":"tls"}`},
		{TypeBark, `{"device_key":"key","level":"unknown"}`}, {TypeBark, `{"device_key":"key","compatibility":"anything"}`},
		{TypeServerChanTurbo, `{"endpoint":"https://other.example","send_key":"key"}`}, {TypeServerChan3, `{"endpoint":"https://host.test","tags":["bad\n"]}`},
		{TypeCustomHTTP, `{"endpoint":"https://{{node.name}}/path"}`}, {TypeCustomHTTP, `{"endpoint":"https://host.test","method":"DELETE"}`},
		{TypeCustomHTTP, `{"endpoint":"https://host.test","auth":{"mode":"oauth"}}`}, {TypeCustomHTTP, `{"endpoint":"https://host.test","named_secrets":{"bad.name":"secret"}}`},
		{TypeCustomHTTP, `{"endpoint":"https://host.test","hmac":{"secret":"key","header":"Host"}}`},
		{"unsupported", `{}`}, {TypeWebhook, `[]`}, {TypeWebhook, `{"unknown":"PRIVATE"}`},
	}
	for _, test := range cases {
		if _, err := CanonicalizeProvider(ConfigWrite{ChannelType: test.kind, Config: json.RawMessage(test.raw), Enabled: true}); err == nil {
			t.Fatalf("invalid %s config accepted", test.kind)
		}
	}
}
