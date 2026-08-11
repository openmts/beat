package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/store"
)

func TestAlertChannelSecretsAreSanitizedAndPreserved(t *testing.T) {
	database := setupTestDB(t)
	channels := store.NewAlertChannelStore(database.DB)
	handler := NewAlertHandler(
		store.NewAlertRuleStore(database.DB),
		channels,
		store.NewAlertEventStore(database.DB),
	)

	createBody := `{"name":"Telegram","channel_type":"telegram","config":"{\"bot_token\":\"123:secret\",\"chat_id\":\"42\"}","enabled":true}`
	created := runHandlerRequest(t, handler.HandleCreateAlertChannel, http.MethodPost, createBody, nil)
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), "123:secret") {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Data model.AlertChannel `json:"data"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode created channel: %v", err)
	}
	channel := createResponse.Data
	if !strings.Contains(channel.Config, "has_bot_token") {
		t.Fatalf("sanitized config = %q", channel.Config)
	}

	updateBody := `{"name":"Telegram","channel_type":"telegram","config":"{\"bot_token\":\"\",\"chat_id\":\"84\"}","enabled":true}`
	updated := runHandlerRequest(t, handler.HandleUpdateAlertChannel, http.MethodPut, updateBody, map[string]string{"id": channel.ID})
	if updated.Code != http.StatusOK || strings.Contains(updated.Body.String(), "123:secret") {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	var updateResponse struct {
		Data model.AlertChannel `json:"data"`
	}
	if err := json.NewDecoder(updated.Body).Decode(&updateResponse); err != nil {
		t.Fatalf("decode updated channel: %v", err)
	}
	if updateResponse.Data.CreatedAt.IsZero() {
		t.Fatal("updated channel lost created_at")
	}
	stored, err := channels.GetAlertChannel(t.Context(), channel.ID)
	if err != nil || stored == nil || !strings.Contains(stored.Config, "123:secret") || !strings.Contains(stored.Config, `"chat_id":"84"`) {
		t.Fatalf("stored channel = %#v, error = %v", stored, err)
	}
}

func TestHandleTestAlertChannelRecordsDelivery(t *testing.T) {
	var delivered model.AlertEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode webhook: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	database := setupTestDB(t)
	channels := store.NewAlertChannelStore(database.DB)
	channel, err := channels.CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Webhook", ChannelType: notification.TypeWebhook,
		Config: `{"url":"` + server.URL + `"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	handler := NewAlertHandler(
		store.NewAlertRuleStore(database.DB),
		channels,
		store.NewAlertEventStore(database.DB),
	)

	response := runHandlerRequest(
		t,
		handler.HandleTestAlertChannel,
		http.MethodPost,
		"",
		map[string]string{"id": channel.ID},
	)
	if response.Code != http.StatusOK || delivered.Message != "Beat test notification" {
		t.Fatalf("status = %d, delivered = %#v, body = %s", response.Code, delivered, response.Body.String())
	}
	list := runHandlerRequest(t, handler.HandleListAlertChannels, http.MethodGet, "", nil)
	if !strings.Contains(list.Body.String(), `"state":"success"`) {
		t.Fatalf("list body = %s", list.Body.String())
	}
}

func TestAlertChannelValidationAndMissingChannel(t *testing.T) {
	database := setupTestDB(t)
	channels := store.NewAlertChannelStore(database.DB)
	handler := NewAlertHandler(
		store.NewAlertRuleStore(database.DB),
		channels,
		store.NewAlertEventStore(database.DB),
	)
	existing, err := channels.CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Existing", ChannelType: notification.TypeWebhook,
		Config: `{"url":"https://example.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create existing channel: %v", err)
	}
	tests := []struct {
		name       string
		method     string
		body       string
		pathValues map[string]string
		call       http.HandlerFunc
		want       int
	}{
		{name: "invalid config", method: http.MethodPost, body: `{"name":"Bad","channel_type":"webhook","config":"ftp://example.com"}`, call: handler.HandleCreateAlertChannel, want: http.StatusBadRequest},
		{name: "malformed update", method: http.MethodPut, body: `{`, pathValues: map[string]string{"id": existing.ID}, call: handler.HandleUpdateAlertChannel, want: http.StatusBadRequest},
		{name: "missing update fields", method: http.MethodPut, body: `{}`, pathValues: map[string]string{"id": existing.ID}, call: handler.HandleUpdateAlertChannel, want: http.StatusBadRequest},
		{name: "invalid update config", method: http.MethodPut, body: `{"name":"Bad","channel_type":"webhook","config":"ftp://example.com"}`, pathValues: map[string]string{"id": existing.ID}, call: handler.HandleUpdateAlertChannel, want: http.StatusBadRequest},
		{name: "missing update", method: http.MethodPut, body: `{"name":"Missing","channel_type":"webhook","config":"https://example.com"}`, pathValues: map[string]string{"id": "missing"}, call: handler.HandleUpdateAlertChannel, want: http.StatusNotFound},
		{name: "missing test", method: http.MethodPost, pathValues: map[string]string{"id": "missing"}, call: handler.HandleTestAlertChannel, want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := runHandlerRequest(t, test.call, test.method, test.body, test.pathValues)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
