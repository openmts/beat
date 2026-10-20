package notification

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestDeclarativeTemplatesRejectCodeAndBoundExpansion(t *testing.T) {
	for _, source := range []string{"{{env.HOME}}", "{{printf \"x\"}}", "{{text", "text }}", "{{secret.token}}", "\x00", strings.Repeat("a", 32769)} {
		if _, err := CompileTemplate(source, false); err == nil {
			t.Fatalf("unsafe template accepted: %q", source[:min(len(source), 50)])
		}
	}
	parts, err := CompileTemplate("hello {{node.name}} {{secret.token}} {{event.status}}", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderDeclarative(parts, TemplateValues{Data: map[string]string{"node.name": "{{env.HOME}}"}, Secrets: map[string]string{"token": "value"}, Maximum: 100})
	if err != nil || result != "hello {{env.HOME}} value " {
		t.Fatal("template evaluated recursively or omitted missing values incorrectly")
	}
	if _, err := RenderDeclarative(parts, TemplateValues{Data: map[string]string{"node.name": strings.Repeat("x", 100)}, Maximum: 20}); err == nil {
		t.Fatal("unbounded expansion")
	}
}

func TestCustomBodiesUseJSONAndURLEncodingAndHMAC(t *testing.T) {
	for _, mode := range []string{"json", "form", "text"} {
		t.Run(mode, func(t *testing.T) {
			body := json.RawMessage(`{"text":"{{text}}","secret":"{{secret.token}}"}`)
			if mode == "text" {
				body = json.RawMessage(`"{{text}} {{secret.token}}"`)
			}
			config := ProviderConfig{Endpoint: "https://receiver.example/path", Method: "POST", BodyMode: mode, Body: body, Query: map[string]string{"value": "{{text}}"}, NamedSecrets: map[string]string{"token": "a&b"},
				Auth: &HTTPAuth{Mode: "bearer", Token: "token"}, HMAC: &HTTPSignature{Secret: "hmac-key", Header: "X-Signature", TimestampHeader: "X-Time"}}
			payload := model.NotificationPayload{Subject: "Subject", Text: "quoted \" text", Data: map[string]string{"text": "quoted \" text"}}
			now := time.Unix(100, 0)
			request, err := buildProviderRequest(t.Context(), requestInput{config: config, payload: payload, now: now, dispatch: model.NotificationDispatch{DeliveryID: "id", ChannelType: TypeCustomHTTP}})
			if err != nil {
				t.Fatal(err)
			}
			if request.URL.Query().Get("value") != payload.Text || request.Header.Get("Authorization") != "Bearer token" {
				t.Fatal("query or auth encoding failed")
			}
			_, encoded, _, err := customRequest(config, payload)
			if err != nil {
				t.Fatal(err)
			}
			mac := hmac.New(sha256.New, []byte("hmac-key"))
			_, _ = mac.Write([]byte("100\nPOST\n" + request.URL.RequestURI() + "\n"))
			_, _ = mac.Write(encoded)
			if request.Header.Get("X-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
				t.Fatal("HMAC did not bind the exact request")
			}
			if mode == "json" && !json.Valid(encoded) {
				t.Fatal("JSON substitution broke escaping")
			}
			if mode == "form" {
				form, err := url.ParseQuery(string(encoded))
				if err != nil || form.Get("secret") != "a&b" {
					t.Fatal("form escaping failed")
				}
			}
		})
	}
}

func TestCustomMethodAndHeaderAndJSONShapeFailures(t *testing.T) {
	for _, config := range []ProviderConfig{
		{Endpoint: "https://receiver.example", Method: "GET", Body: json.RawMessage(`"body"`)},
		{Endpoint: "https://receiver.example", Method: "POST", BodyMode: "form", Body: json.RawMessage(`{"value":1}`)},
		{Endpoint: "https://receiver.example", Method: "POST", BodyMode: "text", Body: json.RawMessage(`{}`)},
		{Endpoint: "https://receiver.example", Method: "POST", BodyMode: "json", Body: json.RawMessage(`{"{{text}}":"value"}`)},
		{Endpoint: "https://receiver.example", Method: "POST", BodyMode: "json", Body: json.RawMessage(`bad`)},
	} {
		if _, _, _, err := customRequest(config, model.NotificationPayload{}); err == nil {
			t.Fatal("invalid custom request accepted")
		}
	}
	request, _ := http.NewRequest(http.MethodPost, "https://receiver.example", nil)
	if err := requestHeaders(request, requestInput{dispatch: model.NotificationDispatch{ChannelType: TypeCustomHTTP}, config: ProviderConfig{Headers: map[string]string{"X-Test": "{{text}}"}}, payload: model.NotificationPayload{Data: map[string]string{"text": "bad\r\nheader"}}}); err == nil {
		t.Fatal("rendered CRLF accepted")
	}
	if _, err := renderPresentation(model.NotificationDispatch{Payload: model.NotificationPayload{Subject: "bad\nheader"}}, ProviderConfig{}); err == nil {
		t.Fatal("subject header injection accepted")
	}
	budget := 0
	if _, err := renderJSONLeaves("text", leafRender{budget: &budget}); err == nil {
		t.Fatal("JSON node budget ignored")
	}
}

func FuzzDeclarativeTemplateNeverExecutesOrExceedsLimit(f *testing.F) {
	f.Add("{{node.name}}")
	f.Add("literal")
	f.Add("{{secret.token}}")
	f.Fuzz(func(t *testing.T, value string) {
		parts, err := CompileTemplate(value, true)
		if err != nil {
			return
		}
		rendered, err := RenderDeclarative(parts, TemplateValues{Data: map[string]string{"node.name": "name"}, Maximum: 1024})
		if err == nil && len(rendered) > 1024 {
			t.Fatal("template size bound violated")
		}
	})
}
