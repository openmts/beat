package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type notificationHandlerFixture struct {
	handler   *NotificationHandler
	db        *sql.DB
	principal model.AdminPrincipal
}

func newNotificationHandlerFixture(t *testing.T) notificationHandlerFixture {
	t.Helper()
	database, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: database.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	now, recent := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	user := model.AdminUser{ID: "owner", Username: "owner", DisplayName: "Owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	session := model.AdminSession{ID: "session", UserID: user.ID, TokenHash: bytes.Repeat([]byte{3}, 32), TokenPrefix: "fixture", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: recent, AbsoluteExpiresAt: recent, ReauthenticatedUntil: &recent}
	admins := store.NewAdminStore(database.DB)
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return notificationHandlerFixture{handler: NewNotificationHandler(queue), db: database.DB, principal: model.AdminPrincipal{User: user, Session: session}}
}

type notificationHandlerCall struct {
	handle       http.HandlerFunc
	method, path string
	id, body     string
	anonymous    bool
}

func (fixture notificationHandlerFixture) call(input notificationHandlerCall) *httptest.ResponseRecorder {
	if input.path == "" {
		input.path = "/api/v1/notifications"
	}
	request := httptest.NewRequest(input.method, "http://example.test"+input.path, strings.NewReader(input.body))
	if input.body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.SetPathValue("id", input.id)
	if !input.anonymous {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), fixture.principal))
	}
	response := httptest.NewRecorder()
	input.handle(response, request)
	return response
}

const notificationChannelBody = `{"name":"Webhook","channel_type":"webhook","enabled":true,"event_kinds":["alert.triggered"],"config":{"endpoint":"https://8.8.8.8/PRIVATE_PATH","headers":{"Authorization":"PRIVATE_TOKEN"}}}`

func (fixture notificationHandlerFixture) create(t *testing.T) model.NotificationChannel {
	t.Helper()
	response := fixture.call(notificationHandlerCall{handle: fixture.handler.HandleCreateChannel, method: http.MethodPost, body: notificationChannelBody})
	if response.Code != http.StatusCreated {
		t.Fatalf("create channel: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "PRIVATE_") {
		t.Fatal("created channel response exposed stored credentials")
	}
	return notificationData[model.NotificationChannel](t, response)
}

func notificationData[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var result struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}
