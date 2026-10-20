package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type metricDashboardFixture struct {
	db        *store.SQLiteStore
	handler   *MetricDashboardHandler
	principal model.AdminPrincipal
	node      *model.Node
}

func newMetricDashboardFixture(t *testing.T) metricDashboardFixture {
	t.Helper()
	db, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	nodes := store.NewNodeStore(db.DB)
	node, err := nodes.UpsertNode(t.Context(), "public-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	principal := dashboardHandlerPrincipal(t, store.NewAdminStore(db.DB))
	return metricDashboardFixture{db: db, principal: principal, node: node,
		handler: NewMetricDashboardHandler(store.NewDashboardStore(db.DB), nodes,
			NewDashboardPolicyReader(store.NewMetricCatalogStore(db.DB), nodes))}
}

func dashboardHandlerPrincipal(t *testing.T, admins *store.AdminStore) model.AdminPrincipal {
	t.Helper()
	now, until := model.NowUTC(), model.NowUTC().Add(time.Hour)
	user := model.AdminUser{ID: "private-admin-id", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner,
		PasswordHash: "test-hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: uuid.NewString(), UserID: user.ID, TokenHash: []byte("test-session-hash"),
		TokenPrefix: "private-prefix", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until,
		ReauthenticatedUntil: &until}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return model.AdminPrincipal{User: user, Session: session}
}

func (fixture metricDashboardFixture) request(t *testing.T, method string, input dashboardTestRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input.body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "http://example.com/dashboard"+input.query, bytes.NewReader(body))
	request.SetPathValue("id", input.id)
	if input.revision != "" {
		request.SetPathValue("revision", input.revision)
	}
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), fixture.principal))
	response := httptest.NewRecorder()
	input.handle(response, request)
	return response
}

type dashboardTestRequest struct {
	body      any
	id, query string
	// revision 是历史版本路由的 {revision} 路径变量，普通看板路由留空。
	revision string
	handle   http.HandlerFunc
}

func dashboardResponse(t *testing.T, response *httptest.ResponseRecorder) model.MetricDashboard {
	t.Helper()
	var body struct {
		Data model.MetricDashboard `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
