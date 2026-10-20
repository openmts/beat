package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// emptyAnnotationList 构造一个没有注释存储的请求，验证协作面按不可用处理。
func emptyAnnotationList(t *testing.T, principal model.AdminPrincipal, handler *DashboardCollaborationHandler) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://example.com/collaboration?dashboard_id=admin-default", nil)
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	handler.HandleListAnnotations(response, request)
	return response
}

func annotationPayload(fixture collaborationFixture, visibility string) map[string]any {
	from := model.NowUTC().Add(-time.Hour)
	return map[string]any{"dashboard_id": fixture.dashboard.ID, "panel_id": fixture.panelID(),
		"title": "deploy window", "text": "rolling restart", "tags": []string{"deploy"},
		"color": "blue", "visibility": visibility, "from": from, "to": from.Add(30 * time.Minute)}
}

func TestAnnotationHandlersRoundTrip(t *testing.T) {
	fixture := newCollaborationFixture(t)
	created := fixture.call(t, http.MethodPost, collaborationRequest{body: annotationPayload(fixture, model.AnnotationVisibilityPublic),
		handle: fixture.collaboration.HandleCreateAnnotation})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body)
	}
	annotation := collaborationData[model.DashboardAnnotation](t, created)
	if annotation.ID == "" || annotation.CreatedBy != fixture.principal.User.ID || annotation.Title != "deploy window" {
		t.Fatalf("annotation = %+v", annotation)
	}
	// 同一看板第二条 private 注释：公开列表里不能出现，管理员列表里必须出现。
	private := fixture.call(t, http.MethodPost, collaborationRequest{body: annotationPayload(fixture, model.AnnotationVisibilityPrivate),
		handle: fixture.collaboration.HandleCreateAnnotation})
	if private.Code != http.StatusCreated {
		t.Fatalf("private create = %d: %s", private.Code, private.Body)
	}
	list := fixture.call(t, http.MethodGet, collaborationRequest{query: "?dashboard_id=" + fixture.dashboard.ID,
		handle: fixture.collaboration.HandleListAnnotations})
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", list.Code, list.Body)
	}
	if values := collaborationData[[]model.DashboardAnnotation](t, list); len(values) != 2 {
		t.Fatalf("admin list = %+v", values)
	}
	public := fixture.call(t, http.MethodGet, collaborationRequest{query: "?dashboard_id=" + fixture.dashboard.ID,
		handle: fixture.collaboration.HandlePublicAnnotations, anonymous: true})
	if public.Code != http.StatusOK || public.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public list = %d %q", public.Code, public.Header().Get("Cache-Control"))
	}
	if values := collaborationData[[]model.DashboardAnnotation](t, public); len(values) != 1 || values[0].Visibility != model.AnnotationVisibilityPublic {
		t.Fatalf("public list = %+v", values)
	}
	// 带显式时间窗口的过滤：窗口外的时间段返回空集合而不是错误。
	window := annotationPayload(fixture, model.AnnotationVisibilityPublic)["from"].(time.Time)
	history := fixture.call(t, http.MethodGet, collaborationRequest{
		query:  "?dashboard_id=" + fixture.dashboard.ID + "&from=" + window.Add(-3*time.Hour).Format(time.RFC3339) + "&to=" + window.Add(-2*time.Hour).Format(time.RFC3339),
		handle: fixture.collaboration.HandleListAnnotations})
	if history.Code != http.StatusOK || len(collaborationData[[]model.DashboardAnnotation](t, history)) != 0 {
		t.Fatalf("windowed list = %d: %s", history.Code, history.Body)
	}
	update := fixture.call(t, http.MethodPut, collaborationRequest{id: annotation.ID, handle: fixture.collaboration.HandleUpdateAnnotation,
		body: map[string]any{"dashboard_id": fixture.dashboard.ID, "panel_id": fixture.panelID(), "title": "renamed window",
			"color": "rose", "visibility": model.AnnotationVisibilityPublic,
			"from": annotation.From, "to": annotation.To}})
	if update.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", update.Code, update.Body)
	}
	if value := collaborationData[model.DashboardAnnotation](t, update); value.Title != "renamed window" {
		t.Fatalf("updated = %+v", value)
	}
	deleted := fixture.call(t, http.MethodDelete, collaborationRequest{id: annotation.ID, handle: fixture.collaboration.HandleDeleteAnnotation})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body)
	}
	missing := fixture.call(t, http.MethodDelete, collaborationRequest{id: annotation.ID, handle: fixture.collaboration.HandleDeleteAnnotation})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d: %s", missing.Code, missing.Body)
	}
}

func TestAnnotationHandlersRejectInvalidRequests(t *testing.T) {
	fixture := newCollaborationFixture(t)
	cases := []struct {
		name     string
		request  collaborationRequest
		expected int
	}{
		{"missing dashboard", collaborationRequest{query: "?dashboard_id=", handle: fixture.collaboration.HandleListAnnotations}, http.StatusBadRequest},
		{"dashboard id too long", collaborationRequest{query: "?dashboard_id=" + strings.Repeat("d", 81), handle: fixture.collaboration.HandleListAnnotations}, http.StatusBadRequest},
		{"unparsable from", collaborationRequest{query: "?dashboard_id=" + fixture.dashboard.ID + "&from=yesterday", handle: fixture.collaboration.HandleListAnnotations}, http.StatusBadRequest},
		{"inverted window", collaborationRequest{query: "?dashboard_id=" + fixture.dashboard.ID + "&from=2026-02-02T00:00:00Z&to=2026-01-01T00:00:00Z", handle: fixture.collaboration.HandleListAnnotations}, http.StatusBadRequest},
		{"unknown body field", collaborationRequest{body: map[string]any{"dashboard_id": fixture.dashboard.ID, "unknown": true},
			handle: fixture.collaboration.HandleCreateAnnotation}, http.StatusBadRequest},
		{"invalid tag", collaborationRequest{body: map[string]any{"dashboard_id": fixture.dashboard.ID, "title": "t",
			"color": "blue", "visibility": model.AnnotationVisibilityPublic, "tags": []string{"Bad Tag"},
			"from": model.NowUTC(), "to": model.NowUTC().Add(time.Minute)}, handle: fixture.collaboration.HandleCreateAnnotation}, http.StatusBadRequest},
		{"invalid palette color", collaborationRequest{body: map[string]any{"dashboard_id": fixture.dashboard.ID, "title": "t",
			"color": "chartreuse", "visibility": model.AnnotationVisibilityPublic,
			"from": model.NowUTC(), "to": model.NowUTC().Add(time.Minute)}, handle: fixture.collaboration.HandleCreateAnnotation}, http.StatusBadRequest},
		{"window too long", collaborationRequest{body: map[string]any{"dashboard_id": fixture.dashboard.ID, "title": "t",
			"color": "blue", "visibility": model.AnnotationVisibilityPublic, "from": model.NowUTC(),
			"to": model.NowUTC().Add(time.Duration(model.MaxAnnotationWindowSeconds+1) * time.Second)},
			handle: fixture.collaboration.HandleCreateAnnotation}, http.StatusBadRequest},
		{"unknown annotation", collaborationRequest{id: "missing-annotation", handle: fixture.collaboration.HandleDeleteAnnotation}, http.StatusNotFound},
		{"unauthenticated list", collaborationRequest{query: "?dashboard_id=admin-default",
			handle: fixture.collaboration.HandleListAnnotations, anonymous: true}, http.StatusUnauthorized},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			method := http.MethodPost
			if testCase.request.handle == nil {
				t.Fatal("missing handler")
			}
			response := fixture.call(t, method, testCase.request)
			if response.Code != testCase.expected {
				t.Fatalf("status = %d: %s", response.Code, response.Body)
			}
		})
	}
	// 缺少存储的实例按不可用处理，而不是 panic。
	empty := &DashboardCollaborationHandler{}
	response := emptyAnnotationList(t, fixture.principal, empty)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable = %d: %s", response.Code, response.Body)
	}
}

func TestSnapshotHandlersShareAndRevoke(t *testing.T) {
	fixture := newCollaborationFixture(t)
	created := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		body: map[string]any{"dashboard_id": fixture.dashboard.ID, "name": "share cpu", "expires_in_seconds": 3600}})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body)
	}
	snapshot := collaborationData[model.DashboardSnapshot](t, created)
	if snapshot.Token == "" || snapshot.ExpiresAt == nil || snapshot.DashboardID != fixture.dashboard.ID {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	list := fixture.call(t, http.MethodGet, collaborationRequest{handle: fixture.collaboration.HandleListSnapshots})
	if values := collaborationData[[]model.DashboardSnapshot](t, list); list.Code != http.StatusOK || len(values) != 1 {
		t.Fatalf("list = %d: %s", list.Code, list.Body)
	}
	// 公开读取不要求登录，也不回显创建者；令牌长度不对按不存在处理。
	public := fixture.call(t, http.MethodGet, collaborationRequest{token: snapshot.Token,
		handle: fixture.collaboration.HandlePublicSnapshot, anonymous: true})
	if public.Code != http.StatusOK || public.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public = %d: %s", public.Code, public.Body)
	}
	shared := collaborationData[model.DashboardSnapshot](t, public)
	if shared.CreatedBy != "" || shared.Token != "" {
		t.Fatalf("public snapshot leaked identity: %+v", shared)
	}
	if short := fixture.call(t, http.MethodGet, collaborationRequest{token: "tooshort",
		handle: fixture.collaboration.HandlePublicSnapshot, anonymous: true}); short.Code != http.StatusNotFound {
		t.Fatalf("short token = %d: %s", short.Code, short.Body)
	}
	revoked := fixture.call(t, http.MethodPost, collaborationRequest{id: snapshot.ID, handle: fixture.collaboration.HandleRevokeSnapshot})
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body)
	}
	if after := fixture.call(t, http.MethodGet, collaborationRequest{token: snapshot.Token,
		handle: fixture.collaboration.HandlePublicSnapshot, anonymous: true}); after.Code != http.StatusNotFound {
		t.Fatalf("revoked snapshot = %d: %s", after.Code, after.Body)
	}
	// 别人的个人看板不能生成快照；非法有效期与非法名字同样被拒。
	foreignActor, _ := fixture.sessionActor(t, "snapshot-outsider", model.AdminRoleAdmin, true)
	foreign := model.DefaultMetricDashboard("personal")
	foreign.ID, foreign.OwnerUserID = uuid.NewString(), foreignActor.UserID
	if _, err := fixture.handler.dashboards.Save(t.Context(), store.DashboardWrite{Value: foreign, Actor: foreignActor}); err != nil {
		t.Fatal(err)
	}
	denied := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		body: map[string]any{"dashboard_id": foreign.ID, "name": "foreign"}})
	if denied.Code != http.StatusNotFound {
		t.Fatalf("foreign snapshot = %d: %s", denied.Code, denied.Body)
	}
	for name, body := range map[string]map[string]any{
		"expiry above maximum": {"dashboard_id": fixture.dashboard.ID, "name": "n", "expires_in_seconds": maxSnapshotExpirySeconds + 1},
		"negative expiry":      {"dashboard_id": fixture.dashboard.ID, "name": "n", "expires_in_seconds": -1},
		"missing name":         {"dashboard_id": fixture.dashboard.ID},
		"unknown dashboard":    {"dashboard_id": "unknown-dashboard", "name": "n"},
	} {
		response := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot, body: body})
		expected := http.StatusBadRequest
		if name == "unknown dashboard" {
			expected = http.StatusNotFound
		}
		if response.Code != expected {
			t.Fatalf("%s = %d: %s", name, response.Code, response.Body)
		}
	}
	// 未登录不能创建快照。
	anonymous := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		body: map[string]any{"dashboard_id": fixture.dashboard.ID, "name": "n"}, anonymous: true})
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous snapshot = %d: %s", anonymous.Code, anonymous.Body)
	}
}

func TestLibraryPanelHandlersRoundTrip(t *testing.T) {
	fixture := newCollaborationFixture(t)
	panel := fixture.dashboard.Definition.Panels[0]
	created := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateLibraryPanel,
		body: map[string]any{"name": "cpu panel", "description": "reusable", "panel": panel}})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body)
	}
	saved := collaborationData[model.DashboardLibraryPanel](t, created)
	if saved.ID == "" || saved.Name != "cpu panel" {
		t.Fatalf("panel = %+v", saved)
	}
	list := fixture.call(t, http.MethodGet, collaborationRequest{handle: fixture.collaboration.HandleListLibraryPanels})
	if list.Code != http.StatusOK || len(collaborationData[[]model.DashboardLibraryPanel](t, list)) != 1 {
		t.Fatalf("list = %d: %s", list.Code, list.Body)
	}
	update := fixture.call(t, http.MethodPut, collaborationRequest{id: saved.ID, handle: fixture.collaboration.HandleUpdateLibraryPanel,
		body: map[string]any{"name": "cpu panel v2", "panel": panel}})
	if update.Code != http.StatusOK || collaborationData[model.DashboardLibraryPanel](t, update).Name != "cpu panel v2" {
		t.Fatalf("update = %d: %s", update.Code, update.Body)
	}
	// 非法面板定义（未知类型）与超长名字都被拒。
	broken := panel
	broken.Type = "sparkline"
	rejected := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateLibraryPanel,
		body: map[string]any{"name": "broken", "panel": broken}})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("broken panel = %d: %s", rejected.Code, rejected.Body)
	}
	deleted := fixture.call(t, http.MethodDelete, collaborationRequest{id: saved.ID, handle: fixture.collaboration.HandleDeleteLibraryPanel})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body)
	}
	if missing := fixture.call(t, http.MethodDelete, collaborationRequest{id: saved.ID, handle: fixture.collaboration.HandleDeleteLibraryPanel}); missing.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d: %s", missing.Code, missing.Body)
	}
	if unknown := fixture.call(t, http.MethodPut, collaborationRequest{id: "missing-panel", handle: fixture.collaboration.HandleUpdateLibraryPanel,
		body: map[string]any{"name": "n", "panel": panel}}); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown update = %d: %s", unknown.Code, unknown.Body)
	}
}

func TestPanelAlertHandlersReplaceAndList(t *testing.T) {
	fixture := newCollaborationFixture(t)
	fixture.insertAlertRule(t, "rule-one")
	replace := fixture.call(t, http.MethodPut, collaborationRequest{id: fixture.dashboard.ID, panelID: fixture.panelID(),
		handle: fixture.collaboration.HandleReplacePanelAlerts, body: map[string]any{"rule_ids": []string{"rule-one"}}})
	if replace.Code != http.StatusOK {
		t.Fatalf("replace = %d: %s", replace.Code, replace.Body)
	}
	values := collaborationData[[]model.DashboardPanelAlert](t, replace)
	if len(values) != 1 || values[0].RuleID != "rule-one" || values[0].DashboardID != fixture.dashboard.ID {
		t.Fatalf("panel alerts = %+v", values)
	}
	list := fixture.call(t, http.MethodGet, collaborationRequest{id: fixture.dashboard.ID, handle: fixture.collaboration.HandleListPanelAlerts})
	if list.Code != http.StatusOK || len(collaborationData[[]model.DashboardPanelAlert](t, list)) != 1 {
		t.Fatalf("list = %d: %s", list.Code, list.Body)
	}
	// 空列表清空关联；未知规则与超量规则被拒。
	cleared := fixture.call(t, http.MethodPut, collaborationRequest{id: fixture.dashboard.ID, panelID: fixture.panelID(),
		handle: fixture.collaboration.HandleReplacePanelAlerts, body: map[string]any{}})
	if cleared.Code != http.StatusOK || len(collaborationData[[]model.DashboardPanelAlert](t, cleared)) != 0 {
		t.Fatalf("cleared = %d: %s", cleared.Code, cleared.Body)
	}
	unknown := fixture.call(t, http.MethodPut, collaborationRequest{id: fixture.dashboard.ID, panelID: fixture.panelID(),
		handle: fixture.collaboration.HandleReplacePanelAlerts, body: map[string]any{"rule_ids": []string{"missing-rule"}}})
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown rule = %d: %s", unknown.Code, unknown.Body)
	}
	tooMany := []string{"r1", "r2", "r3", "r4", "r5"}
	overflow := fixture.call(t, http.MethodPut, collaborationRequest{id: fixture.dashboard.ID, panelID: fixture.panelID(),
		handle: fixture.collaboration.HandleReplacePanelAlerts, body: map[string]any{"rule_ids": tooMany}})
	if overflow.Code != http.StatusBadRequest {
		t.Fatalf("too many rules = %d: %s", overflow.Code, overflow.Body)
	}
	if missing := fixture.call(t, http.MethodPut, collaborationRequest{id: "missing-dashboard", panelID: "p",
		handle: fixture.collaboration.HandleReplacePanelAlerts, body: map[string]any{"rule_ids": []string{}}}); missing.Code != http.StatusNotFound {
		t.Fatalf("missing dashboard = %d: %s", missing.Code, missing.Body)
	}
	if anonymous := fixture.call(t, http.MethodGet, collaborationRequest{id: fixture.dashboard.ID,
		handle: fixture.collaboration.HandleListPanelAlerts, anonymous: true}); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d: %s", anonymous.Code, anonymous.Body)
	}
}
