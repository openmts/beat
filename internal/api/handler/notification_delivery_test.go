package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func TestNotificationDeliveryHandlersRetryCancelAndPaginate(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	channel := fixture.create(t)
	tested := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleTestChannel, method: http.MethodPost, id: channel.ID})
	receipt := notificationData[notificationqueue.Receipt](t, tested)
	id := receipt.Deliveries[0].ID
	if _, err := fixture.db.Exec("UPDATE notification_deliveries SET state='dead',completed_at=1 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleRetry, method: http.MethodPost, id: id, body: `{"confirm_duplicate":true}`})
	if response.Code != http.StatusAccepted {
		t.Fatalf("manual retry: %d %s", response.Code, response.Body.String())
	}
	retry := notificationData[model.NotificationDeliveryRef](t, response)
	if retry.ID == id || retry.State != "pending" {
		t.Fatal("retry overwrote the original delivery")
	}
	for _, call := range []notificationHandlerCall{
		{handle: fixture.handler.HandleListDeliveries, method: http.MethodGet, path: "/api/v1/notifications/deliveries?state=dead&limit=10&offset=0&channel_id=" + channel.ID},
		{handle: fixture.handler.HandleGetDelivery, method: http.MethodGet, id: id},
		{handle: fixture.handler.HandleUpdateSettings, method: http.MethodPut, body: `{"expected_revision":1,"success_retention_days":45,"dead_retention_days":90,"maximum_workers":4,"egress_policy":{"exceptions":[]}}`},
	} {
		if response := fixture.call(call); response.Code != http.StatusOK {
			t.Fatalf("notification read/update: %d %s", response.Code, response.Body.String())
		}
	}
	response = fixture.call(notificationHandlerCall{handle: fixture.handler.HandleCancel, method: http.MethodPost, id: retry.ID})
	if response.Code != http.StatusNoContent {
		t.Fatal("pending retry was not cancelled")
	}
}

func TestNotificationReadAndMutationErrorsReturnStableStatuses(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	for _, query := range []string{"?limit=bad", "?offset=bad", "?limit=101", "?state=invalid"} {
		response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleListDeliveries, method: http.MethodGet, path: "/api/v1/notifications/deliveries" + query})
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid delivery filter was accepted")
		}
	}
	for _, call := range []notificationHandlerCall{
		{handle: fixture.handler.HandleGetChannel, method: http.MethodGet},
		{handle: fixture.handler.HandleGetDelivery, method: http.MethodGet},
		{handle: fixture.handler.HandleDeleteChannel, method: http.MethodDelete},
		{handle: fixture.handler.HandleSetEnabled, method: http.MethodPatch, body: `{"enabled":true}`},
		{handle: fixture.handler.HandleTestChannel, method: http.MethodPost},
		{handle: fixture.handler.HandleRetry, method: http.MethodPost, body: `{"confirm_duplicate":true}`},
		{handle: fixture.handler.HandleCancel, method: http.MethodPost},
	} {
		call.id = "missing"
		if response := fixture.call(call); response.Code != http.StatusNotFound {
			t.Fatalf("missing notification record: %d", response.Code)
		}
	}
	for _, test := range []struct {
		err    error
		status int
	}{{notificationqueue.ErrConflict, 409}, {notificationqueue.ErrQueueCapacity, 503}, {errors.New("PRIVATE_STORAGE_DETAIL"), 503}} {
		response := httptest.NewRecorder()
		notificationError(response, test.err)
		if response.Code != test.status {
			t.Fatal("notification error status changed")
		}
	}
}

func TestNotificationHandlersFailClosedWhenStorageUnavailable(t *testing.T) {
	fixture := newNotificationHandlerFixture(t)
	channel := fixture.create(t)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, call := range []notificationHandlerCall{
		{handle: fixture.handler.HandleListChannels, method: http.MethodGet},
		{handle: fixture.handler.HandleGetChannel, method: http.MethodGet},
		{handle: fixture.handler.HandleListDeliveries, method: http.MethodGet},
		{handle: fixture.handler.HandleGetDelivery, method: http.MethodGet},
		{handle: fixture.handler.HandleSettings, method: http.MethodGet},
		{handle: fixture.handler.HandleCreateChannel, method: http.MethodPost, body: notificationChannelBody},
		{handle: fixture.handler.HandleUpdateChannel, method: http.MethodPut, body: `{"name":"Webhook","channel_type":"webhook","expected_revision":1,"config":{}}`},
		{handle: fixture.handler.HandleUpdateSettings, method: http.MethodPut, body: `{"expected_revision":1,"success_retention_days":30,"dead_retention_days":90,"maximum_workers":8}`},
		{handle: fixture.handler.HandlePreview, method: http.MethodPost, body: `{"channel_type":"webhook","config":{}}`},
	} {
		call.id = channel.ID
		if response := fixture.call(call); response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable notification storage: %d %s", response.Code, response.Body.String())
		}
	}
}
