package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestServiceSendWebhookAndRecordStatus(t *testing.T) {
	var delivered model.AlertEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode webhook: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{ID: "webhook", ChannelType: TypeWebhook, Config: `{"url":"` + server.URL + `"}`}
	event := testEvent()
	status, err := service.Send(t.Context(), channel, event)
	if err != nil || status.State != DeliverySuccess || delivered.ID != event.ID {
		t.Fatalf("status = %#v, delivered = %#v, error = %v", status, delivered, err)
	}
	if got := service.Status(channel.ID); got == nil || got.State != DeliverySuccess {
		t.Fatalf("stored status = %#v", got)
	}
	if stats := service.Stats(); stats.Success != 1 || stats.Failed != 0 {
		t.Fatalf("delivery stats = %#v", stats)
	}
	service.Forget(channel.ID)
	if got := service.Status(channel.ID); got != nil {
		t.Fatalf("status after Forget = %#v", got)
	}
}

func TestServiceStatsRecordFailure(t *testing.T) {
	service := NewService()
	channel := &model.AlertChannel{ID: "invalid", ChannelType: "invalid", Config: `{}`}
	if _, err := service.Send(t.Context(), channel, testEvent()); err == nil {
		t.Fatal("invalid delivery succeeded")
	}
	if stats := service.Stats(); stats.Success != 0 || stats.Failed != 1 {
		t.Fatalf("delivery stats = %#v", stats)
	}
}

func TestServiceSendMessageKeepsAlertWebhookContractSeparate(t *testing.T) {
	var delivered Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode report webhook: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "report", ChannelType: TypeWebhook, Config: `{"url":"` + server.URL + `"}`,
	}
	message := Message{
		Kind: "traffic_report", Subject: "Daily traffic", Text: "report body",
		Data: map[string]string{"period": "2026-07-29"},
	}
	status, err := service.SendMessage(t.Context(), channel, message)
	if err != nil || status.State != DeliverySuccess {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
	if delivered.Kind != message.Kind || delivered.Subject != message.Subject || delivered.Text != message.Text {
		t.Fatalf("delivered message = %#v", delivered)
	}
}

func TestServiceSendMessageUsesTextForTelegramAndEmail(t *testing.T) {
	service := NewService()
	var subject, body string
	service.emailSender = func(_ context.Context, _ EmailConfig, gotSubject, gotBody string) error {
		subject, body = gotSubject, gotBody
		return nil
	}
	channel := &model.AlertChannel{
		ID: "email", ChannelType: TypeEmail,
		Config: `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`,
	}
	message := Message{Kind: "traffic_report", Subject: "Daily traffic", Text: "report body"}
	if _, err := service.SendMessage(t.Context(), channel, message); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if subject != message.Subject || body != message.Text {
		t.Fatalf("email subject = %q, body = %q", subject, body)
	}
}

func TestServiceSendTelegram(t *testing.T) {
	var path string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode telegram: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	service := NewService()
	service.telegramBaseURL = server.URL
	channel := &model.AlertChannel{ID: "telegram", ChannelType: TypeTelegram, Config: `{"bot_token":"token","chat_id":"42"}`}
	if _, err := service.Send(t.Context(), channel, testEvent()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if path != "/bottoken/sendMessage" || payload["chat_id"] != "42" || !strings.Contains(payload["text"].(string), "test alert") {
		t.Fatalf("path = %q, payload = %#v", path, payload)
	}
	if _, err := service.SendMessage(
		t.Context(), channel, Message{Kind: "traffic_report", Subject: "Report", Text: "report body"},
	); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if payload["text"] != "report body" {
		t.Fatalf("message payload = %#v", payload)
	}
}

func TestServiceSendEmail(t *testing.T) {
	service := NewService()
	var got EmailConfig
	service.emailSender = func(_ context.Context, config EmailConfig, subject, body string) error {
		got = config
		if !strings.Contains(subject, "triggered") || !strings.Contains(body, "test alert") {
			t.Fatalf("subject = %q, body = %q", subject, body)
		}
		return nil
	}
	channel := &model.AlertChannel{ID: "email", ChannelType: TypeEmail, Config: `{"host":"smtp.example.com","port":587,"username":"beat","password":"secret","from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`}
	if _, err := service.Send(t.Context(), channel, testEvent()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.Password != "secret" || got.Port != 587 {
		t.Fatalf("email config = %#v", got)
	}
}

func TestServiceRecordsFailures(t *testing.T) {
	service := NewService()
	service.now = func() time.Time { return time.Date(2026, 7, 30, 3, 0, 0, 0, time.UTC) }
	service.emailSender = func(context.Context, EmailConfig, string, string) error {
		return errors.New("smtp unavailable")
	}
	channel := &model.AlertChannel{ID: "email", ChannelType: TypeEmail, Config: `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["ops@example.com"],"security":"starttls"}`}
	status, err := service.Send(t.Context(), channel, testEvent())
	if err == nil || status.State != DeliveryFailed || status.Message != "email request failed: transport error" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
	if !status.DeliveredAt.Equal(service.now()) {
		t.Fatalf("delivered_at = %v", status.DeliveredAt)
	}
}

func TestServiceRejectsUnknownChannel(t *testing.T) {
	service := NewService()
	channel := &model.AlertChannel{ID: "unknown", ChannelType: "sms", Config: `{}`}
	if status, err := service.Send(t.Context(), channel, testEvent()); err == nil || status.State != DeliveryFailed {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
	if status, err := service.SendMessage(t.Context(), channel, Message{}); err == nil || status.State != DeliveryFailed {
		t.Fatalf("message status = %#v, error = %v", status, err)
	}
}

func TestServiceHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	serverURL := server.URL
	server.Close()

	tests := []struct {
		name    string
		channel model.AlertChannel
	}{
		{name: "unreachable webhook", channel: model.AlertChannel{ID: "w", ChannelType: TypeWebhook, Config: `{"url":"` + serverURL + `"}`}},
		{name: "unreachable Telegram", channel: model.AlertChannel{ID: "t", ChannelType: TypeTelegram, Config: `{"bot_token":"123:token","chat_id":"42"}`}},
		{name: "invalid email config", channel: model.AlertChannel{ID: "e", ChannelType: TypeEmail, Config: `{}`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService()
			service.telegramBaseURL = serverURL
			if _, err := service.Send(t.Context(), &test.channel, testEvent()); err == nil {
				t.Fatal("Send() error = nil")
			}
		})
	}

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	service := NewService()
	channel := model.AlertChannel{ID: "w", ChannelType: TypeWebhook, Config: `{"url":"` + server.URL + `"}`}
	if _, err := service.Send(t.Context(), &channel, testEvent()); err == nil {
		t.Fatal("Send() status error = nil")
	}
}

func testEvent() *model.AlertEvent {
	return &model.AlertEvent{
		ID: "event", RuleID: "rule", NodeID: "node", Message: "test alert",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
	}
}

func TestServiceSendWebhookWithTemplate(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "tmpl", ChannelType: TypeWebhook,
		Config: `{"url":"` + server.URL + `","template":"alert [{{severity}}] {{rule}} node={{node}} {{metric}}={{value}} {{status}}"}`,
	}
	rich := &model.AlertEvent{
		ID: "web", RuleID: "rule", NodeID: "node", Message: "test alert",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
		RuleName: "High CPU", NodeName: "web-01", Metric: "cpu",
		Severity: model.AlertSeverityCritical,
	}
	if _, err := service.Send(t.Context(), channel, rich); err != nil {
		t.Fatalf("send: %v", err)
	}
	want := "alert [critical] High CPU node=web-01 cpu=95.00 triggered"
	if received != want {
		t.Fatalf("received = %q, want %q", received, want)
	}
}

func TestServiceSendTelegramTemplate(t *testing.T) {
	var received struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode telegram: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	service.telegramBaseURL = server.URL
	channel := &model.AlertChannel{
		ID: "tg", ChannelType: TypeTelegram,
		Config: `{"bot_token":"tok","chat_id":"42","template":"{{rule}}: {{metric}}={{value}}"}`,
	}
	rich := &model.AlertEvent{
		ID: "echo", RuleID: "rule", NodeID: "node", Message: "test alert",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
		RuleName: "High CPU", NodeName: "web-01", Metric: "cpu",
		Severity: model.AlertSeverityCritical,
	}
	if _, err := service.Send(t.Context(), channel, rich); err != nil {
		t.Fatalf("send: %v", err)
	}
	if received.Text != "High CPU: cpu=95.00" {
		t.Fatalf("text = %q", received.Text)
	}
}

func TestServiceSendWebhookWithCustomHeaders(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "hdr", ChannelType: TypeWebhook,
		Config: `{"url":"` + server.URL + `","headers":{"Authorization":"Bearer secret","X-Custom":"abc"}}`,
	}
	if _, err := service.Send(t.Context(), channel, testEvent()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := gotHeaders.Get("X-Custom"); got != "abc" {
		t.Fatalf("X-Custom = %q", got)
	}
	if gotHeaders.Get("Content-Type") == "" {
		t.Fatal("Content-Type should be set")
	}
}

func TestServiceSendWebhookErrorIncludesStatusWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid signature"))
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "err", ChannelType: TypeWebhook, Config: `{"url":"` + server.URL + `"}`,
	}
	status, err := service.Send(t.Context(), channel, testEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") || strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("error = %v, want status 400 without untrusted response body", err)
	}
	if status.State != DeliveryFailed || status.Message != err.Error() {
		t.Fatalf("status = %#v, want failed with detail", status)
	}
}

func TestServiceSendWebhookTransportErrorIncludesCause(t *testing.T) {
	service := NewService()
	// Unreachable address forces a transport error.
	channel := &model.AlertChannel{
		ID: "conn", ChannelType: TypeWebhook, Config: `{"url":"http://127.0.0.1:1"}`,
	}
	_, err := service.Send(t.Context(), channel, testEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "webhook request failed") {
		t.Fatalf("error = %v, want webhook request failed prefix", err)
	}
}

func TestDeliveryTextFallsBackToStructuredSummary(t *testing.T) {
	event := &model.AlertEvent{
		RuleID: "mock-rule", NodeID: "mock-node", Value: 42,
		Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
	}
	text := deliveryText(event)
	if !strings.Contains(text, "mock-rule") || !strings.Contains(text, "mock-node") ||
		!strings.Contains(text, "42.00") {
		t.Fatalf("text = %q, want structured summary", text)
	}
}

func TestDeliveryTextIncludesTimeAndSeverity(t *testing.T) {
	event := &model.AlertEvent{
		RuleName: "High CPU", NodeName: "web-01", Value: 95,
		Severity: model.AlertSeverityCritical, TriggeredAt: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}
	text := deliveryText(event)
	if !strings.Contains(text, "[critical]") || !strings.Contains(text, "High CPU") ||
		!strings.Contains(text, "web-01") || !strings.Contains(text, "2026-08-18T12:00:00Z") {
		t.Fatalf("text = %q", text)
	}
}

func TestServiceSendTelegramMarkdownV2(t *testing.T) {
	var received struct {
		ChatID    string `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode telegram: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	service.telegramBaseURL = server.URL
	rich := &model.AlertEvent{
		ID: "e", RuleID: "rule", NodeID: "node", Message: "high",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
		RuleName: "high.cpu", NodeName: "web-01", Metric: "cpu_used",
		Severity: model.AlertSeverityWarning,
	}
	channel := &model.AlertChannel{
		ID: "tg-md", ChannelType: TypeTelegram,
		Config: `{"bot_token":"tok","chat_id":"42","parse_mode":"MarkdownV2","template":"*{{rule}}* {{node}} {{value}}"}`,
	}
	if _, err := service.Send(t.Context(), channel, rich); err != nil {
		t.Fatalf("send: %v", err)
	}
	if received.ParseMode != "MarkdownV2" {
		t.Fatalf("parse_mode = %q", received.ParseMode)
	}
	// Dynamic value rule (high.cpu) is escaped, literal markup (*) is preserved.
	if received.Text != `*high\.cpu* web\-01 95\.00` {
		t.Fatalf("text = %q", received.Text)
	}
}

func TestServiceSendWebhookSmartJSONTemplate(t *testing.T) {
	var gotCT string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "json-tmpl", ChannelType: TypeWebhook,
		Config: `{"url":"` + server.URL + `","template":"{\"chat_id\":\"42\",\"text\":\"{{rule}} {{value}}\"}"}`,
	}
	rich := &model.AlertEvent{ID: "e", RuleID: "r", NodeID: "n", Message: "m",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
		RuleName: "High CPU", NodeName: "web-01", Metric: "cpu", Severity: model.AlertSeverityWarning,
	}
	if _, err := service.Send(t.Context(), channel, rich); err != nil {
		t.Fatalf("send: %v", err)
	}
	// JSON template is detected and sent as application/json.
	if !strings.Contains(gotCT, "application/json") {
		t.Fatalf("Content-Type = %q, want json", gotCT)
	}
	var parsed map[string]any
	if err := json.Unmarshal(gotBody, &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if parsed["chat_id"] != "42" || parsed["text"] != "High CPU 95.00" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestServiceSendWebhookTextTemplateAsPlain(t *testing.T) {
	var gotCT string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	service := NewService()
	channel := &model.AlertChannel{
		ID: "text-tmpl", ChannelType: TypeWebhook,
		Config: `{"url":"` + server.URL + `","template":"[{{severity}}] {{rule}} on {{node}}"}`,
	}
	rich := &model.AlertEvent{ID: "e", RuleID: "r", NodeID: "n", Message: "m",
		Value: 95, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
		RuleName: "High CPU", NodeName: "web-01", Metric: "cpu", Severity: model.AlertSeverityWarning,
	}
	if _, err := service.Send(t.Context(), channel, rich); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(gotCT, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", gotCT)
	}
	if gotBody != "[warning] High CPU on web-01" {
		t.Fatalf("body = %q", gotBody)
	}
}
