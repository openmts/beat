package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestProviderLimitsRejectInvalidBoundsAndOptionalFields(t *testing.T) {
	cases := []struct{ kind, raw string }{
		{TypeWebhook, `{"endpoint":"https://receiver.example","timeout_seconds":31}`},
		{TypeWebhook, `{"endpoint":"https://receiver.example","redirects":4}`},
		{TypeWebhook, `{"endpoint":"https://receiver.example","headers":{"Bad Header":"value"}}`},
		{TypeWebhook, `{"endpoint":"https://receiver.example","headers":{"X-Test":"{{unknown}}"}}`},
		{TypeTelegram, `{"bot_token":"token","chat_id":"chat","endpoint":"https://api.telegram.org/path"}`},
		{TypeTelegram, `{"bot_token":"token?query","chat_id":"chat"}`},
		{TypeBark, `{"device_key":"key","icon":"http://icons.example"}`},
		{TypeBark, `{"device_key":"key","url":"invalid"}`},
		{TypeBark, `{"device_key":"key","sound":"bad\n"}`},
		{TypeServerChanTurbo, `{"send_key":"bad?key"}`},
		{TypeServerChan3, `{"endpoint":"https://receiver.example","tags":["1","2","3","4","5","6","7","8","9"]}`},
		{TypeEmail, `{"host":"bad host","port":465,"security":"tls"}`},
		{TypeEmail, `{"host":"smtp.example","port":70000,"security":"tls"}`},
		{TypeEmail, `{"host":"smtp.example","port":465,"security":"invalid"}`},
		{TypeEmail, `{"host":"smtp.example","port":465,"security":"tls","username":"bad\n"}`},
		{TypeCustomHTTP, `{"endpoint":"https://receiver.example","body_mode":"invalid"}`},
		{TypeCustomHTTP, `{"endpoint":"https://receiver.example","query":{"value":"{{unknown}}"}}`},
		{TypeCustomHTTP, `{"endpoint":"https://receiver.example","auth":{"mode":"bearer","token":"key"},"headers":{"Authorization":"Bearer other"}}`},
	}
	for _, test := range cases {
		if _, err := CanonicalizeProvider(ConfigWrite{ChannelType: test.kind, Config: json.RawMessage(test.raw)}); err == nil {
			t.Fatalf("invalid optional configuration accepted for %s", test.kind)
		}
	}
}

func TestProviderDecoderAndExplicitNestedSecretClearing(t *testing.T) {
	for _, raw := range []string{"null", "[]", `{} {}`, `{"unknown":true}`, strings.Repeat("x", MaximumConfigBytes+1)} {
		if _, err := DecodeProvider([]byte(raw)); err == nil {
			t.Fatal("provider decoder accepted malformed or unbounded input")
		}
	}
	first, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeCustomHTTP, Config: json.RawMessage(`{"endpoint":"https://receiver.example","named_secrets":{"key":"PRIVATE_KEY"},"auth":{"mode":"basic","username":"name","password":"PRIVATE_PASSWORD"},"hmac":{"secret":"PRIVATE_HMAC"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeCustomHTTP, Previous: first.Secret, ClearSecrets: []string{"auth.password", "hmac.secret", "named_secrets.key"}})
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeProvider(cleared.Secret)
	if err != nil || config.Auth.Password != "" || config.HMAC.Secret != "" || len(config.NamedSecrets) != 0 {
		t.Fatal("explicit nested clear retained stored credentials")
	}
	if fmt.Sprint(config) != "notification provider configuration" {
		t.Fatal("provider formatting exposed stored fields")
	}
}

func TestStructuredRenderingBoundsTotalExpansionAndDepth(t *testing.T) {
	config := ProviderConfig{Endpoint: "https://receiver.example", Method: "POST", BodyMode: "json", Body: json.RawMessage(`["{{text}}","{{text}}","{{text}}","{{text}}","{{text}}"]`)}
	if _, _, _, err := customRequest(config, model.NotificationPayload{Data: map[string]string{"text": strings.Repeat("x", 16*1024)}}); err == nil {
		t.Fatal("structured rendering exceeded its aggregate memory budget")
	}
	config.Body = json.RawMessage(`[{"value":"{{text}}"},42,true,null]`)
	if _, body, _, err := customRequest(config, model.NotificationPayload{Data: map[string]string{"text": "message"}}); err != nil || !json.Valid(body) {
		t.Fatal("bounded nested JSON values were not preserved")
	}
	config.Body = json.RawMessage(strings.Repeat("[", maximumJSONDepth+2) + `"text"` + strings.Repeat("]", maximumJSONDepth+2))
	if _, _, _, err := customRequest(config, model.NotificationPayload{}); err == nil {
		t.Fatal("deep JSON exceeded the traversal limit")
	}
	config.Method, config.Body, config.Query = "GET", nil, map[string]string{"message": "{{text}}"}
	if _, _, _, err := customRequest(config, model.NotificationPayload{Data: map[string]string{"text": strings.Repeat("x", maximumEndpointBytes+1)}}); err == nil {
		t.Fatal("query expansion exceeded the request URL limit")
	}
}
