package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func TestNotificationChannelHandlersLifecycleAndSafeResponses(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	channel := fixture.create(t)
	for _, call := range []notificationHandlerCall{
		{handle: fixture.handler.HandleListChannels, method: http.MethodGet},
		{handle: fixture.handler.HandleGetChannel, method: http.MethodGet, id: channel.ID},
		{handle: fixture.handler.HandleEventKinds, method: http.MethodGet},
		{handle: fixture.handler.HandleSettings, method: http.MethodGet},
		{handle: fixture.handler.HandlePreview, method: http.MethodPost, body: `{"channel_type":"custom_http","config":{"endpoint":"https://8.8.8.8/PRIVATE_PATH","body":{"key":"PRIVATE_BODY","text":"{{text}}"}}}`},
		{handle: fixture.handler.HandleSetEnabled, method: http.MethodPatch, id: channel.ID, body: `{"enabled":false}`},
	} {
		response := fixture.call(call)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "PRIVATE_") {
			t.Fatalf("unsafe channel response: %d %s", response.Code, response.Body.String())
		}
	}
	write := model.NotificationChannelWrite{Name: "Updated", ChannelType: "webhook", Enabled: false, ExpectedRevision: 1, Config: json.RawMessage(`{"endpoint":""}`)}
	encoded, err := json.Marshal(write)
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleUpdateChannel, method: http.MethodPut, id: channel.ID, body: string(encoded)})
	if response.Code != http.StatusOK || notificationData[model.NotificationChannel](t, response).CurrentRevision != 2 {
		t.Fatal("channel update did not create an immutable revision")
	}
	response = fixture.call(notificationHandlerCall{handle: fixture.handler.HandleTestChannel, method: http.MethodPost, id: channel.ID})
	if response.Code != http.StatusAccepted || notificationData[notificationqueue.Receipt](t, response).State != "pending" {
		t.Fatal("test send bypassed durable asynchronous admission")
	}
	response = fixture.call(notificationHandlerCall{handle: fixture.handler.HandleDeleteChannel, method: http.MethodDelete, id: channel.ID})
	if response.Code != http.StatusNoContent || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("channel deletion response was not private")
	}
}

func TestNotificationMutationHandlersRequirePrincipalAndValidJSON(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	for _, handle := range []http.HandlerFunc{fixture.handler.HandleCreateChannel, fixture.handler.HandleUpdateChannel, fixture.handler.HandleSetEnabled, fixture.handler.HandlePreview, fixture.handler.HandleRetry, fixture.handler.HandleUpdateSettings} {
		for _, anonymous := range []bool{false, true} {
			response := fixture.call(notificationHandlerCall{handle: handle, method: http.MethodPost, body: "invalid", anonymous: anonymous})
			want := http.StatusBadRequest
			if anonymous {
				want = http.StatusUnauthorized
			}
			if response.Code != want {
				t.Fatalf("mutation validation: expected %d, got %d", want, response.Code)
			}
		}
	}
	for _, handle := range []http.HandlerFunc{fixture.handler.HandleDeleteChannel, fixture.handler.HandleTestChannel, fixture.handler.HandleCancel} {
		response := fixture.call(notificationHandlerCall{handle: handle, method: http.MethodPost, anonymous: true})
		if response.Code != http.StatusUnauthorized {
			t.Fatal("mutation accepted an unauthenticated principal")
		}
	}
}

func TestNotificationHandlersRecheckStoredAuthorityAndBudgets(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	channel := fixture.create(t)
	for index := range 6 {
		response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleTestChannel, method: http.MethodPost, id: channel.ID})
		want := http.StatusAccepted
		if index == 5 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatal("test send budget was not enforced")
		}
	}
	if _, err := fixture.db.Exec("UPDATE admin_users SET role='admin'"); err != nil {
		t.Fatal(err)
	}
	response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleCreateChannel, method: http.MethodPost, body: notificationChannelBody})
	if response.Code != http.StatusForbidden {
		t.Fatal("stale owner principal changed channel configuration")
	}
	if _, err := fixture.db.Exec("UPDATE admin_users SET role='owner'"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	response = fixture.call(notificationHandlerCall{handle: fixture.handler.HandleDeleteChannel, method: http.MethodDelete, id: channel.ID})
	if response.Code != http.StatusPreconditionRequired {
		t.Fatal("expired proof deleted a channel")
	}
}
