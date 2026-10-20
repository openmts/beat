package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type adminSecurityEventHandlerFixture struct {
	securityEventsFixture
	principal model.AdminPrincipal
}

func newAdminSecurityEventHandlerFixture(t *testing.T) adminSecurityEventHandlerFixture {
	t.Helper()
	base := newSecurityEventsFixture(t)
	return adminSecurityEventHandlerFixture{securityEventsFixture: base,
		principal: dashboardHandlerPrincipal(t, base.admins)}
}

// handle 直接调用处理器方法；路由中间件（认证、角色与最近认证）在路由层用例中覆盖。
func (fixture adminSecurityEventHandlerFixture) handle(
	t *testing.T,
	method string,
	path string,
	body string,
	values map[string]string,
	call func(http.ResponseWriter, *http.Request),
) *httptest.ResponseRecorder {
	t.Helper()
	request := newAdminRequest(t, &fixture.principal, method, path, body)
	for key, value := range values {
		request.SetPathValue(key, value)
	}
	response := httptest.NewRecorder()
	call(response, request)
	return response
}

func decodeAdminSecurityData(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode security envelope: %v body = %s", err, response.Body.String())
	}
	if envelope.Code != response.Code {
		t.Fatalf("envelope code = %d, response code = %d", envelope.Code, response.Code)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("decode security data: %v body = %s", err, response.Body.String())
	}
}

func TestAdminSecurityHandlerListsAndReadsEvents(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	seedSecurityEvent(t, fixture.securityEventsFixture, "event-list", "audit-list")

	listed := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events", "", nil,
		fixture.handler.HandleListEvents)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listed.Code, listed.Body.String())
	}
	var page model.SecurityEventPage
	decodeAdminSecurityData(t, listed, &page)
	if len(page.Events) != 1 || page.Events[0].ID != "event-list" {
		t.Fatalf("listed events = %+v", page.Events)
	}
	// 摘要由服务端渲染：前端只展示标签，不解析原始类别值。
	if page.Events[0].SourceSummary == "" || page.Events[0].DeviceSummary == "" {
		t.Fatalf("event summaries = %+v", page.Events[0])
	}

	filtered := fixture.handle(t, http.MethodGet,
		"/api/v1/admin/security/events?kind="+model.SecurityEventLoginSucceeded+"&severity="+
			model.SecuritySeverityMedium+"&source="+model.SecuritySourceClassPublic+
			"&routing="+model.SecurityRoutingNotRouted+"&delivery=&limit=10&offset=0", "", nil,
		fixture.handler.HandleListEvents)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered list status = %d body = %s", filtered.Code, filtered.Body.String())
	}
	decodeAdminSecurityData(t, filtered, &page)
	if len(page.Events) != 1 {
		t.Fatalf("filtered events = %+v", page.Events)
	}
	// 事件的创建时间早于当前时钟，后端必须把它夹到 lookback 窗口内而不是返回空页。
	windowed := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events?limit=1&offset=1", "", nil,
		fixture.handler.HandleListEvents)
	if windowed.Code != http.StatusOK {
		t.Fatalf("paged list status = %d body = %s", windowed.Code, windowed.Body.String())
	}

	detail := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events/event-list", "",
		map[string]string{"id": "event-list"}, fixture.handler.HandleEventDetail)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d body = %s", detail.Code, detail.Body.String())
	}
	var event model.SecurityEvent
	decodeAdminSecurityData(t, detail, &event)
	if event.ID != "event-list" || event.SourceSummary == "" {
		t.Fatalf("detail event = %+v", event)
	}
	missing := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events/absent", "",
		map[string]string{"id": "absent"}, fixture.handler.HandleEventDetail)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing detail status = %d body = %s", missing.Code, missing.Body.String())
	}
}

func TestAdminSecurityHandlerValidatesEventFilters(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	for _, query := range []string{"?limit=0", "?limit=201", "?limit=abc", "?offset=-1", "?offset=abc"} {
		response := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events"+query, "", nil,
			fixture.handler.HandleListEvents)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d body = %s", query, response.Code, response.Body.String())
		}
	}
	if _, err := securityLimit("200"); err != nil {
		t.Fatalf("limit 200 rejected: %v", err)
	}
	if _, err := securityOffset("0"); err != nil {
		t.Fatalf("offset 0 rejected: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/events?actor=owner&delivery=routed", nil)
	filter, err := securityEventFilter(request)
	if err != nil {
		t.Fatalf("read filter: %v", err)
	}
	if filter.ActorID != "owner" || filter.DeliveryState != "routed" {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestAdminSecurityHandlerAcknowledgesEvents(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	seedSecurityEvent(t, fixture.securityEventsFixture, "event-ack", "audit-ack")
	acknowledged := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/events/event-ack/acknowledge",
		`{"note":"reviewed by operator"}`, map[string]string{"id": "event-ack"},
		fixture.handler.HandleAcknowledgeEvent)
	if acknowledged.Code != http.StatusOK {
		t.Fatalf("acknowledge status = %d body = %s", acknowledged.Code, acknowledged.Body.String())
	}
	var event model.SecurityEvent
	decodeAdminSecurityData(t, acknowledged, &event)
	if event.AcknowledgedAt == nil || event.AcknowledgedBy != fixture.principal.User.ID {
		t.Fatalf("acknowledged event = %+v", event)
	}
	// 重复确认是冲突，必须提示前端刷新而不是静默重写确认人。
	repeated := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/events/event-ack/acknowledge",
		`{"note":"again"}`, map[string]string{"id": "event-ack"}, fixture.handler.HandleAcknowledgeEvent)
	if repeated.Code != http.StatusConflict {
		t.Fatalf("repeated acknowledge status = %d body = %s", repeated.Code, repeated.Body.String())
	}
	malformed := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/events/event-ack/acknowledge",
		`{"note":`, map[string]string{"id": "event-ack"}, fixture.handler.HandleAcknowledgeEvent)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed acknowledge status = %d body = %s", malformed.Code, malformed.Body.String())
	}
}

func TestAdminSecurityHandlerReadsAndUpdatesPolicy(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	read := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/policy", "", nil,
		fixture.handler.HandlePolicy)
	if read.Code != http.StatusOK {
		t.Fatalf("policy status = %d body = %s", read.Code, read.Body.String())
	}
	var settings model.SecurityNotificationSettings
	decodeAdminSecurityData(t, read, &settings)
	if settings.Revision == 0 || settings.UpdatedAt.IsZero() {
		t.Fatalf("policy = %+v", settings)
	}

	write := model.SecurityNotificationSettingsWrite{ExpectedRevision: settings.Revision,
		EnabledEventKinds: settings.EnabledEventKinds, PrivacyLevel: settings.PrivacyLevel,
		LookbackDays: settings.LookbackDays, ThresholdFailures: settings.ThresholdFailures,
		ThresholdWindowSecond: settings.ThresholdWindowSecond, InvalidTOTPThreshold: settings.InvalidTOTPThreshold,
		RetentionDays: settings.RetentionDays}
	payload, err := json.Marshal(write)
	if err != nil {
		t.Fatalf("encode policy write: %v", err)
	}
	updated := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/policy", string(payload), nil,
		fixture.handler.HandleUpdatePolicy)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updated.Code, updated.Body.String())
	}
	var after model.SecurityNotificationSettings
	decodeAdminSecurityData(t, updated, &after)
	if after.Revision != settings.Revision+1 || after.UpdatedBy != fixture.principal.User.ID {
		t.Fatalf("updated policy = %+v", after)
	}
	// 使用过期 revision 的第二次写入必须冲突。
	stale := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/policy", string(payload), nil,
		fixture.handler.HandleUpdatePolicy)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d body = %s", stale.Code, stale.Body.String())
	}
	malformed := fixture.handle(t, http.MethodPost, "/api/v1/admin/security/policy", `{"privacy_level":`,
		nil, fixture.handler.HandleUpdatePolicy)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed policy status = %d body = %s", malformed.Code, malformed.Body.String())
	}
}

func TestAdminSecurityHandlerOptionsAreStatic(t *testing.T) {
	response := httptest.NewRecorder()
	NewAdminSecurityHandler(nil).HandleOptions(response, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/security/options", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("options status = %d", response.Code)
	}
	var options securityOptionsResponse
	decodeAdminSecurityData(t, response, &options)
	if len(options.Kinds) == 0 || len(options.Severities) == 0 || len(options.PrivacyLevels) == 0 {
		t.Fatalf("options = %+v", options)
	}
	if strings.Contains(response.Body.String(), `"`+model.SecurityPrivacyFullSource+`":"`) {
		t.Fatalf("options leak machine keys: %s", response.Body.String())
	}
}

func TestAdminSecurityHandlerRejectsAnonymousRequests(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	handlers := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"list", fixture.handler.HandleListEvents},
		{"detail", fixture.handler.HandleEventDetail},
		{"acknowledge", fixture.handler.HandleAcknowledgeEvent},
		{"policy", fixture.handler.HandlePolicy},
		{"update-policy", fixture.handler.HandleUpdatePolicy},
		{"source", fixture.handler.HandleEventSource},
	}
	for _, entry := range handlers {
		request := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/admin/security/events", nil)
		response := httptest.NewRecorder()
		entry.call(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d body = %s", entry.name, response.Code, response.Body.String())
		}
	}
}

func TestWriteAdminSecurityErrorMapsStoreFailures(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	response := httptest.NewRecorder()
	fixture.handler.HandleEventSource(response, httptest.NewRequest(http.MethodGet,
		"http://example.com/api/v1/admin/security/events/absent/source", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous source status = %d", response.Code)
	}
}

func TestAdminSecurityHandlerPolicyRequiresOwner(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	viewer := fixture.principal
	viewer.User.Role = model.AdminRoleAdmin
	request := newAdminRequest(t, &viewer, http.MethodGet, "/api/v1/admin/security/policy", "")
	response := httptest.NewRecorder()
	fixture.handler.HandlePolicy(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer policy status = %d body = %s", response.Code, response.Body.String())
	}
	update := newAdminRequest(t, &viewer, http.MethodPost, "/api/v1/admin/security/policy",
		`{"expected_revision":1}`)
	updateResponse := httptest.NewRecorder()
	fixture.handler.HandleUpdatePolicy(updateResponse, update)
	if updateResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer update status = %d body = %s", updateResponse.Code, updateResponse.Body.String())
	}
}

func TestAdminSecurityHandlerReportsClosedHistoryStore(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	if err := fixture.database.Close(); err != nil {
		t.Fatalf("close security store: %v", err)
	}
	listed := fixture.handle(t, http.MethodGet, "/api/v1/admin/security/events", "", nil,
		fixture.handler.HandleListEvents)
	if listed.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed list status = %d body = %s", listed.Code, listed.Body.String())
	}
}

func TestWriteAdminSecurityErrorMapsEveryFailure(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"missing-event", store.ErrSecurityEventNotFound, http.StatusNotFound},
		{"missing-source", store.ErrSecurityEventSourceUnavailable, http.StatusConflict},
		{"already-acknowledged", store.ErrSecurityEventAcknowledged, http.StatusConflict},
		{"stale-revision", store.ErrSecuritySettingsRevision, http.StatusConflict},
		{"settings-invalid", store.ErrSecuritySettingsInvalid, http.StatusBadRequest},
		{"filter-invalid", store.ErrSecurityFilterInvalid, http.StatusBadRequest},
		{"event-invalid", store.ErrSecurityEventInvalid, http.StatusBadRequest},
		{"threshold-invalid", store.ErrSecurityThresholdInvalid, http.StatusBadRequest},
		{"forbidden", adminauth.ErrForbidden, http.StatusForbidden},
		{"recent-auth", adminauth.ErrRecentAuthRequired, http.StatusPreconditionRequired},
		{"unknown", errors.New("security history exploded"), http.StatusServiceUnavailable},
	}
	for _, testCase := range cases {
		response := httptest.NewRecorder()
		writeAdminSecurityError(response, testCase.err)
		if response.Code != testCase.status {
			t.Fatalf("%s status = %d body = %s", testCase.name, response.Code, response.Body.String())
		}
	}
}

func TestAdminSecurityEventSourceHandlerRequiresOwner(t *testing.T) {
	fixture := newAdminSecurityEventHandlerFixture(t)
	viewer := fixture.principal
	viewer.User.Role = model.AdminRoleAdmin
	response := fixture.source(t, &viewer, "event-any")
	if response.Code != http.StatusForbidden {
		t.Fatalf("admin source status = %d body = %s", response.Code, response.Body.String())
	}
}
