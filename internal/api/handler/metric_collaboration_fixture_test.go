package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type collaborationFixture struct {
	metricDashboardFixture
	collaboration *DashboardCollaborationHandler
	dashboard     model.MetricDashboard
}

// newCollaborationFixture 在指标看板夹具之上补一个真实保存过的个人看板，
// 协作面（注释/快照/面板库/面板级告警）都要求目标看板已经落库。
func newCollaborationFixture(t *testing.T) collaborationFixture {
	t.Helper()
	base := newMetricDashboardFixture(t)
	created := dashboardResponse(t, base.request(t, http.MethodPost,
		dashboardTestRequest{body: dashboardRequest{Definition: model.DefaultMetricDashboard("personal").Definition},
			handle: base.handler.HandleCreate}))
	return collaborationFixture{metricDashboardFixture: base, dashboard: created,
		collaboration: NewDashboardCollaborationHandler(store.NewDashboardStore(base.db.DB),
			store.NewDashboardAnnotationStore(base.db.DB), store.NewDashboardSnapshotStore(base.db.DB),
			store.NewDashboardPanelLibraryStore(base.db.DB), store.NewDashboardPanelAlertStore(base.db.DB))}
}

type collaborationRequest struct {
	body      any
	query     string
	id        string
	panelID   string
	token     string
	handle    http.HandlerFunc
	actor     *model.AdminPrincipal
	anonymous bool
}

func (fixture collaborationFixture) call(t *testing.T, method string, input collaborationRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input.body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "http://example.com/collaboration"+input.query, bytes.NewReader(body))
	for key, value := range map[string]string{"id": input.id, "panelId": input.panelID, "token": input.token} {
		if value != "" {
			request.SetPathValue(key, value)
		}
	}
	if !input.anonymous {
		principal := fixture.principal
		if input.actor != nil {
			principal = *input.actor
		}
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	}
	response := httptest.NewRecorder()
	input.handle(response, request)
	return response
}

func collaborationData[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

// withPanels 是协作面的稳定测试看板：首个面板 id 直接来自默认定义。
func (fixture collaborationFixture) panelID() string {
	return fixture.dashboard.Definition.Panels[0].ID
}

func (fixture collaborationFixture) insertAlertRule(t *testing.T, id string) {
	t.Helper()
	if _, err := fixture.db.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,severity,enabled,created_at,updated_at)
		VALUES(?,?,'cpu','gt',80,'warning',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, id, "rule "+id); err != nil {
		t.Fatal(err)
	}
}
