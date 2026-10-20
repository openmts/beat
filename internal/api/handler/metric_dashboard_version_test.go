package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestMetricDashboardVersionHistoryListAndRestore(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("personal").Definition
	definition.Title = "first layout"
	create := fixture.request(t, http.MethodPost, dashboardTestRequest{body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleCreate})
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body)
	}
	created := dashboardResponse(t, create)
	definition.Title = "second layout"
	update := fixture.request(t, http.MethodPut, dashboardTestRequest{id: created.ID,
		body: dashboardRequest{Revision: 1, Definition: definition, Summary: "raise cpu panel"}, handle: fixture.handler.HandleUpdate})
	if update.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", update.Code, update.Body)
	}
	versions := fixture.request(t, http.MethodGet, dashboardTestRequest{id: created.ID, handle: fixture.handler.HandleListVersions})
	if versions.Code != http.StatusOK {
		t.Fatalf("list versions = %d: %s", versions.Code, versions.Body)
	}
	listed := dashboardVersionsResponse(t, versions)
	if len(listed) != 2 || listed[0].Revision != 2 || listed[1].Revision != 1 {
		t.Fatalf("versions = %+v", listed)
	}
	if listed[0].Summary != "raise cpu panel" || listed[0].CreatedBy != fixture.principal.User.Username {
		t.Fatalf("version metadata = %+v", listed[0])
	}
	// 定义体不随列表返回：历史版本只暴露元数据，读取完整定义必须走单版本接口。
	if strings.Contains(versions.Body.String(), "series") || strings.Contains(versions.Body.String(), "definition_json") {
		t.Fatalf("list leaked definitions: %s", versions.Body)
	}
	old := fixture.request(t, http.MethodGet, dashboardTestRequest{id: created.ID, revision: "1", handle: fixture.handler.HandleVersionDefinition})
	if old.Code != http.StatusOK {
		t.Fatalf("version definition = %d: %s", old.Code, old.Body)
	}
	if definitionResponse(t, old).Title != "first layout" {
		t.Fatalf("version definition = %s", old.Body)
	}
	restored := fixture.request(t, http.MethodPost, dashboardTestRequest{id: created.ID, revision: "1",
		body: map[string]any{"revision": 2, "summary": "演习恢复"}, handle: fixture.handler.HandleRestoreVersion})
	if restored.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restored.Code, restored.Body)
	}
	value := dashboardResponse(t, restored)
	if value.Revision != 3 || value.Definition.Title != "first layout" {
		t.Fatalf("restored = %+v", value)
	}
	// 恢复产生新版本而不是覆盖历史，因此历史仍是三条且最新一条记录恢复备注。
	listed = dashboardVersionsResponse(t, fixture.request(t, http.MethodGet, dashboardTestRequest{id: created.ID, handle: fixture.handler.HandleListVersions}))
	if len(listed) != 3 || listed[0].Revision != 3 || listed[0].Summary != "演习恢复" {
		t.Fatalf("versions after restore = %+v", listed)
	}
}

func TestMetricDashboardVersionRejectsInvalidRequests(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	definition := model.DefaultMetricDashboard("personal").Definition
	created := dashboardResponse(t, fixture.request(t, http.MethodPost,
		dashboardTestRequest{body: dashboardRequest{Definition: definition}, handle: fixture.handler.HandleCreate}))
	cases := []struct {
		name     string
		request  dashboardTestRequest
		expected int
	}{
		{"non numeric revision", dashboardTestRequest{id: created.ID, revision: "abc", handle: fixture.handler.HandleVersionDefinition}, http.StatusBadRequest},
		{"zero revision", dashboardTestRequest{id: created.ID, revision: "0", handle: fixture.handler.HandleVersionDefinition}, http.StatusBadRequest},
		{"unknown revision", dashboardTestRequest{id: created.ID, revision: "99", handle: fixture.handler.HandleVersionDefinition}, http.StatusNotFound},
		{"unknown dashboard", dashboardTestRequest{id: "missing-dashboard", handle: fixture.handler.HandleListVersions}, http.StatusNotFound},
		{"stale expected revision", dashboardTestRequest{id: created.ID, revision: "1",
			body: map[string]any{"revision": 7}, handle: fixture.handler.HandleRestoreVersion}, http.StatusConflict},
		{"unknown restore target", dashboardTestRequest{id: created.ID, revision: "5",
			body: map[string]any{"revision": 1}, handle: fixture.handler.HandleRestoreVersion}, http.StatusNotFound},
		{"control characters in summary", dashboardTestRequest{id: created.ID, revision: "1",
			body: map[string]any{"revision": 1, "summary": "bad\x00summary"}, handle: fixture.handler.HandleRestoreVersion}, http.StatusBadRequest},
		{"summary too long", dashboardTestRequest{id: created.ID, revision: "1",
			body: map[string]any{"revision": 1, "summary": strings.Repeat("测", model.MaxDashboardVersionSummaryBytes)}, handle: fixture.handler.HandleRestoreVersion}, http.StatusBadRequest},
		{"unknown body field", dashboardTestRequest{id: created.ID, revision: "1",
			body: map[string]any{"revision": 1, "unexpected": true}, handle: fixture.handler.HandleRestoreVersion}, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.request(t, http.MethodPost, testCase.request)
			if response.Code != testCase.expected {
				t.Fatalf("status = %d: %s", response.Code, response.Body)
			}
		})
	}
}

func TestMetricDashboardVersionEnforcesDashboardScope(t *testing.T) {
	fixture := newMetricDashboardFixture(t)
	// 别人的个人看板对当前主体完全不可见：列表、读取与恢复都必须 404，而不是暴露存在性。
	outsiderActor, _ := fixture.sessionActor(t, "outsider", model.AdminRoleAdmin, true)
	foreign := model.DefaultMetricDashboard("personal")
	foreign.ID, foreign.OwnerUserID = uuid.NewString(), outsiderActor.UserID
	saved, err := fixture.handler.dashboards.Save(t.Context(), store.DashboardWrite{Value: foreign, Actor: outsiderActor})
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]dashboardTestRequest{
		"list":    {id: saved.ID, handle: fixture.handler.HandleListVersions},
		"read":    {id: saved.ID, revision: "1", handle: fixture.handler.HandleVersionDefinition},
		"restore": {id: saved.ID, revision: "1", body: map[string]any{"revision": 1}, handle: fixture.handler.HandleRestoreVersion},
	} {
		response := fixture.request(t, http.MethodPost, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s foreign dashboard = %d: %s", name, response.Code, response.Body)
		}
	}
	// 共享看板：只读历史要求 owner，但不要求最近认证；非 owner 一律 403。
	public := model.DefaultMetricDashboard("public").Definition
	updated := fixture.request(t, http.MethodPut, dashboardTestRequest{id: model.PublicDashboardID,
		body: dashboardRequest{Definition: public}, handle: fixture.handler.HandleUpdate})
	if updated.Code != http.StatusOK {
		t.Fatalf("publish = %d: %s", updated.Code, updated.Body)
	}
	_, operator := fixture.sessionActor(t, "operator", model.AdminRoleAdmin, true)
	denied := fixture.requestAs(t, operator, http.MethodGet, dashboardTestRequest{id: model.PublicDashboardID, handle: fixture.handler.HandleListVersions})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non owner list = %d: %s", denied.Code, denied.Body)
	}
	if list := fixture.request(t, http.MethodGet, dashboardTestRequest{id: model.PublicDashboardID, handle: fixture.handler.HandleListVersions}); list.Code != http.StatusOK {
		t.Fatalf("owner list = %d: %s", list.Code, list.Body)
	}
	// 恢复是写动作：owner 但会话不满足最近认证时返回 428，与既有发布路径一致。
	_, staleOwner := fixture.sessionActor(t, "stale-owner", model.AdminRoleOwner, false)
	restore := fixture.requestAs(t, staleOwner, http.MethodPost, dashboardTestRequest{id: model.PublicDashboardID, revision: "1",
		body: map[string]any{"revision": 1}, handle: fixture.handler.HandleRestoreVersion})
	if restore.Code != http.StatusPreconditionRequired {
		t.Fatalf("stale session restore = %d: %s", restore.Code, restore.Body)
	}
}

// sessionActor 造一个额外管理员（用户 + 会话）并返回存储主体与 HTTP 主体；recent 决定会话是否
// 满足最近认证。版本历史的只读路径只要 owner，写路径才要求最近认证。
func (fixture metricDashboardFixture) sessionActor(t *testing.T, username string, role model.AdminRole, recent bool) (store.DashboardActor, model.AdminPrincipal) {
	t.Helper()
	admins := store.NewAdminStore(fixture.db.DB)
	now := model.NowUTC()
	user := model.AdminUser{ID: uuid.NewString(), Username: username, DisplayName: username, Role: role,
		PasswordHash: "test-hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: uuid.NewString(), UserID: user.ID, TokenHash: []byte(uuid.NewString()),
		TokenPrefix: username, CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)}
	if recent {
		until := now.Add(time.Hour)
		session.ReauthenticatedUntil = &until
	}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	actor := store.DashboardActor{UserID: user.ID, SessionID: session.ID, RequestID: "request-id",
		IPAddress: "127.0.0.1", UserAgent: "test"}
	return actor, model.AdminPrincipal{User: user, Session: session}
}

func (fixture metricDashboardFixture) requestAs(t *testing.T, principal model.AdminPrincipal, method string, input dashboardTestRequest) *httptest.ResponseRecorder {
	t.Helper()
	saved := fixture.principal
	fixture.principal = principal
	defer func() { fixture.principal = saved }()
	return fixture.request(t, method, input)
}

func dashboardVersionsResponse(t *testing.T, response *httptest.ResponseRecorder) []model.MetricDashboardVersion {
	t.Helper()
	var body struct {
		Data []model.MetricDashboardVersion `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

func definitionResponse(t *testing.T, response *httptest.ResponseRecorder) model.DashboardDefinition {
	t.Helper()
	var body struct {
		Data model.DashboardDefinition `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
