package notification

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestNotificationResponseDoesNotExposeSecretEchoes(t *testing.T) {
	const secret = "fixture-private-credential"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("rejected " + secret + " " + r.URL.String()))
	}))
	t.Cleanup(server.Close)
	for _, channel := range []model.AlertChannel{
		{ID: "webhook", ChannelType: TypeWebhook, Config: `{"url":"` + server.URL + `/hook?token=` + secret + `"}`},
		{ID: "telegram", ChannelType: TypeTelegram, Config: `{"bot_token":"` + secret + `","chat_id":"42"}`},
	} {
		service := NewService()
		service.telegramBaseURL = server.URL
		status, err := service.Send(t.Context(), &channel, testEvent())
		if err == nil || status.State != DeliveryFailed || !strings.Contains(status.Message, "400") {
			t.Fatal("delivery failure was not reported")
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(status.Message, secret) {
			t.Fatal("provider error leaked a configured credential")
		}
		if strings.Contains(service.Status(channel.ID).Message, secret) {
			t.Fatal("delivery status retained a provider secret")
		}
	}
}

func TestTelegramTransportFailureDoesNotExposeTokenURL(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	service := NewService()
	service.telegramBaseURL = server.URL
	const secret = "fixture-private-token"
	channel := &model.AlertChannel{ID: "telegram", ChannelType: TypeTelegram, Config: `{"bot_token":"` + secret + `","chat_id":"42"}`}
	status, err := service.Send(t.Context(), channel, testEvent())
	if err == nil || status.State != DeliveryFailed {
		t.Fatal("missing transport error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(status.Message, secret) {
		t.Fatal("transport error exposed the token-bearing URL")
	}
}

func TestSMTPFailureDoesNotExposeCredential(t *testing.T) {
	service := NewService()
	const secret = "fixture-smtp-password"
	service.emailSender = func(context.Context, EmailConfig, string, string) error {
		return errors.New("authentication failed with " + secret)
	}
	channel := &model.AlertChannel{ID: "smtp", ChannelType: TypeEmail,
		Config: `{"host":"smtp.example.com","port":587,"username":"fixture","password":"` + secret + `","from":"a@example.com","to":["b@example.com"],"security":"starttls"}`}
	status, err := service.Send(t.Context(), channel, testEvent())
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(status.Message, secret) {
		t.Fatal("SMTP error was not safely reported")
	}
}

func TestNotificationRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)
	service := NewService()
	channel := &model.AlertChannel{ID: "redirect", ChannelType: TypeWebhook,
		Config: `{"url":"` + source.URL + `","headers":{"X-Api-Key":"fixture-secret"}}`}
	status, err := service.Send(t.Context(), channel, testEvent())
	if err == nil || status.State != DeliveryFailed || forwarded.Load() != 0 {
		t.Fatal("notification credentials followed an unapproved redirect")
	}
}
