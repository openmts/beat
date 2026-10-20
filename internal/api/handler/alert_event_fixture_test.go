package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

type manualEventQueue struct {
	database *sql.DB
	request  notificationqueue.Request
	failure  error
	before   func()
}

func (queue *manualEventQueue) Enqueue(ctx context.Context, request notificationqueue.Request, mutation func(*sql.Tx) error) (notificationqueue.Receipt, error) {
	if queue.before != nil {
		queue.before()
	}
	transaction, err := queue.database.BeginTx(ctx, nil)
	if err != nil {
		return notificationqueue.Receipt{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := mutation(transaction); err != nil {
		if errors.Is(err, notificationqueue.ErrSkipMutation) {
			return notificationqueue.Receipt{Skipped: true}, nil
		}
		return notificationqueue.Receipt{}, err
	}
	if queue.failure != nil {
		return notificationqueue.Receipt{}, queue.failure
	}
	if err := transaction.Commit(); err != nil {
		return notificationqueue.Receipt{}, err
	}
	queue.request = request
	return notificationqueue.Receipt{MessageID: "message", State: "pending"}, nil
}

type manualEventFixture struct {
	database  *store.SQLiteStore
	handler   *AlertHandler
	principal model.AdminPrincipal
	queue     *manualEventQueue
	event     model.AlertEvent
}

func newManualEventFixture(t *testing.T) manualEventFixture {
	t.Helper()
	database := setupTestDB(t)
	principal := dashboardHandlerPrincipal(t, store.NewAdminStore(database.DB))
	events := store.NewAlertEventStore(database.DB)
	event := model.AlertEvent{ID: "event", RuleID: "rule", NodeID: "node", Message: "high usage", Value: 53, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC()}
	if err := events.CreateEvent(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	queue := &manualEventQueue{database: database.DB}
	handler := NewAlertHandler(store.NewAlertRuleStore(database.DB), store.NewAlertChannelStore(database.DB), events, store.NewNodeStore(database.DB))
	handler.SetNotifications(service.NewAlertNotificationService(events, queue))
	return manualEventFixture{database: database, handler: handler, principal: principal, queue: queue, event: event}
}

func (fixture manualEventFixture) request(t *testing.T, target model.AlertStatus) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "http://beat.test/events/event", nil)
	request.SetPathValue("id", fixture.event.ID)
	if fixture.principal.User.ID != "" {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), fixture.principal))
	}
	response := httptest.NewRecorder()
	if target == model.AlertStatusAcknowledged {
		fixture.handler.HandleAcknowledgeAlertEvent(response, request)
	} else {
		fixture.handler.HandleResolveAlertEvent(response, request)
	}
	return response
}

func queuedEventResponse(t *testing.T, response *httptest.ResponseRecorder) AlertEventChangeResult {
	t.Helper()
	var body struct {
		Data AlertEventChangeResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
