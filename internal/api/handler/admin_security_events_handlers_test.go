package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

// securityEventCall 直接调用处理器，路由中间件不在本层用例范围内。
func securityEventCall(
	t *testing.T,
	fixture securityEventsFixture,
	principal *model.AdminPrincipal,
	method string,
	path string,
	body string,
	id string,
	handle http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	request := newAdminRequest(t, principal, method, path, body)
	if id != "" {
		request.SetPathValue("id", id)
	}
	response := httptest.NewRecorder()
	handle(response, request)
	return response
}

func decodeSecurityEventPage(t *testing.T, response *httptest.ResponseRecorder) model.SecurityEventPage {
	t.Helper()
	var body struct {
		Data model.SecurityEventPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return body.Data
}

func decodeSecurityEvent(t *testing.T, response *httptest.ResponseRecorder) model.SecurityEvent {
	t.Helper()
	var body struct {
		Data model.SecurityEvent `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return body.Data
}

func TestAdminSecurityEventHandlerListRejectsInvalidInput(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	unauthenticated := securityEventCall(t, fixture, nil, http.MethodGet,
		"/api/v1/admin/security/events", "", "", fixture.handler.HandleListEvents)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d", unauthenticated.Code)
	}
	for name, query := range map[string]string{
		"limit text":    "?limit=abc",
		"limit zero":    "?limit=0",
		"limit too big": "?limit=201",
		"offset text":   "?offset=abc",
		"offset low":    "?offset=-1",
	} {
		t.Run(name, func(t *testing.T) {
			response := securityEventCall(t, fixture, &owner, http.MethodGet,
				"/api/v1/admin/security/events"+query, "", "", fixture.handler.HandleListEvents)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}
	invalid := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/events?severity=critical", "", "", fixture.handler.HandleListEvents)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "security request is invalid") {
		t.Fatalf("invalid filter = %d body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestAdminSecurityEventHandlerListsFilteredEvents(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	seedSecurityEvent(t, fixture, "event-first", "audit-first")
	seedSecurityEvent(t, fixture, "event-second", "")

	response := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/events?kind="+model.SecurityEventLoginSucceeded+"&severity="+model.SecuritySeverityMedium+
			"&source="+model.SecuritySourceClassPublic+"&routing="+model.SecurityRoutingNotRouted+
			"&reason="+model.SecurityReasonNewSourcePrefix+"&delivery=none&limit=2",
		"", "", fixture.handler.HandleListEvents)
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d body = %s", response.Code, response.Body.String())
	}
	page := decodeSecurityEventPage(t, response)
	if page.Total != 2 || len(page.Events) != 2 || page.Limit != 2 {
		t.Fatalf("page = %+v", page)
	}
	for _, event := range page.Events {
		if event.SourceSummary == "" || event.DeviceSummary == "" {
			t.Fatalf("event summaries = %+v", event)
		}
	}
	second := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/events?offset=1&limit=1", "", "", fixture.handler.HandleListEvents)
	if second.Code != http.StatusOK {
		t.Fatalf("paged list = %d body = %s", second.Code, second.Body.String())
	}
	if paged := decodeSecurityEventPage(t, second); len(paged.Events) != 1 || paged.Offset != 1 {
		t.Fatalf("paged = %+v", paged)
	}
}

func TestAdminSecurityEventHandlerEventDetail(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	administrator := owner
	administrator.User.Role = model.AdminRoleAdmin
	seedSecurityEvent(t, fixture, "event-detail", "audit-detail")

	missing := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/events/absent", "", "absent", fixture.handler.HandleEventDetail)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing detail = %d body = %s", missing.Code, missing.Body.String())
	}
	blank := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/events/", "", "", fixture.handler.HandleEventDetail)
	if blank.Code != http.StatusNotFound {
		t.Fatalf("blank detail = %d body = %s", blank.Code, blank.Body.String())
	}
	found := securityEventCall(t, fixture, &administrator, http.MethodGet,
		"/api/v1/admin/security/events/event-detail", "", "event-detail", fixture.handler.HandleEventDetail)
	if found.Code != http.StatusOK {
		t.Fatalf("detail = %d body = %s", found.Code, found.Body.String())
	}
	event := decodeSecurityEvent(t, found)
	if event.ID != "event-detail" || event.SourceSummary == "" || event.DeviceSummary == "" {
		t.Fatalf("detail event = %+v", event)
	}
}

func TestAdminSecurityEventHandlerAcknowledge(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	seedSecurityEvent(t, fixture, "event-ack", "audit-ack")

	cases := []struct {
		name   string
		id     string
		body   string
		status int
	}{
		{"malformed body", "event-ack", "{", http.StatusBadRequest},
		{"unknown field", "event-ack", `{"note":"x","extra":1}`, http.StatusBadRequest},
		{"unknown event", "absent", `{"note":"x"}`, http.StatusNotFound},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			response := securityEventCall(t, fixture, &owner, http.MethodPost,
				"/api/v1/admin/security/events/"+item.id+"/acknowledge", item.body, item.id,
				fixture.handler.HandleAcknowledgeEvent)
			if response.Code != item.status {
				t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}
	acknowledged := securityEventCall(t, fixture, &owner, http.MethodPost,
		"/api/v1/admin/security/events/event-ack/acknowledge", `{"note":"checked"}`, "event-ack",
		fixture.handler.HandleAcknowledgeEvent)
	if acknowledged.Code != http.StatusOK {
		t.Fatalf("acknowledge = %d body = %s", acknowledged.Code, acknowledged.Body.String())
	}
	if event := decodeSecurityEvent(t, acknowledged); event.AcknowledgedAt == nil || event.AcknowledgedNote != "checked" {
		t.Fatalf("acknowledged event = %+v", event)
	}
	again := securityEventCall(t, fixture, &owner, http.MethodPost,
		"/api/v1/admin/security/events/event-ack/acknowledge", `{"note":"again"}`, "event-ack",
		fixture.handler.HandleAcknowledgeEvent)
	if again.Code != http.StatusConflict {
		t.Fatalf("second acknowledge = %d body = %s", again.Code, again.Body.String())
	}
}

func TestAdminSecurityEventHandlerPolicy(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	administrator := owner
	administrator.User.Role = model.AdminRoleAdmin

	read := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/policy", "", "", fixture.handler.HandlePolicy)
	if read.Code != http.StatusOK {
		t.Fatalf("read policy = %d body = %s", read.Code, read.Body.String())
	}
	var readBody struct {
		Data model.SecurityNotificationSettings `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &readBody); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	forbidden := securityEventCall(t, fixture, &administrator, http.MethodGet,
		"/api/v1/admin/security/policy", "", "", fixture.handler.HandlePolicy)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("administrator policy = %d body = %s", forbidden.Code, forbidden.Body.String())
	}

	stale := owner
	stale.Session.ReauthenticatedUntil = nil
	staleWrite := securityPolicyWrite(t, fixture, readBody.Data)
	staleWrite.ExpectedRevision = readBody.Data.Revision
	staleResponse := securityEventCall(t, fixture, &stale, http.MethodPut,
		"/api/v1/admin/security/policy", mustJSON(t, staleWrite), "", fixture.handler.HandleUpdatePolicy)
	if staleResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("stale update = %d body = %s", staleResponse.Code, staleResponse.Body.String())
	}
}

func TestAdminSecurityEventHandlerUpdatePolicy(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	owner := dashboardHandlerPrincipal(t, fixture.admins)
	read := securityEventCall(t, fixture, &owner, http.MethodGet,
		"/api/v1/admin/security/policy", "", "", fixture.handler.HandlePolicy)
	var readBody struct {
		Data model.SecurityNotificationSettings `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &readBody); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	malformed := securityEventCall(t, fixture, &owner, http.MethodPut,
		"/api/v1/admin/security/policy", "{", "", fixture.handler.HandleUpdatePolicy)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed update = %d body = %s", malformed.Code, malformed.Body.String())
	}
	invalid := securityPolicyWrite(t, fixture, readBody.Data)
	invalid.PrivacyLevel = "loud"
	invalidResponse := securityEventCall(t, fixture, &owner, http.MethodPut,
		"/api/v1/admin/security/policy", mustJSON(t, invalid), "", fixture.handler.HandleUpdatePolicy)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid update = %d body = %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	conflict := securityPolicyWrite(t, fixture, readBody.Data)
	conflict.ExpectedRevision = readBody.Data.Revision + 5
	conflictResponse := securityEventCall(t, fixture, &owner, http.MethodPut,
		"/api/v1/admin/security/policy", mustJSON(t, conflict), "", fixture.handler.HandleUpdatePolicy)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict update = %d body = %s", conflictResponse.Code, conflictResponse.Body.String())
	}
	updated := securityEventCall(t, fixture, &owner, http.MethodPut,
		"/api/v1/admin/security/policy", mustJSON(t, securityPolicyWrite(t, fixture, readBody.Data)), "",
		fixture.handler.HandleUpdatePolicy)
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d body = %s", updated.Code, updated.Body.String())
	}
	var updatedBody struct {
		Data model.SecurityNotificationSettings `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &updatedBody); err != nil {
		t.Fatalf("decode updated policy: %v", err)
	}
	if updatedBody.Data.Revision != readBody.Data.Revision+1 {
		t.Fatalf("updated revision = %d", updatedBody.Data.Revision)
	}
}

func TestAdminSecurityEventHandlerOptions(t *testing.T) {
	fixture := newSecurityEventsFixture(t)
	response := securityEventCall(t, fixture, nil, http.MethodGet,
		"/api/v1/admin/security/options", "", "", fixture.handler.HandleOptions)
	if response.Code != http.StatusOK {
		t.Fatalf("options = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Data securityOptionsResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	if len(body.Data.Kinds) == 0 || len(body.Data.Reasons) == 0 || len(body.Data.PrivacyLevels) == 0 {
		t.Fatalf("options = %+v", body.Data)
	}
}

func securityPolicyWrite(
	t *testing.T,
	fixture securityEventsFixture,
	settings model.SecurityNotificationSettings,
) model.SecurityNotificationSettingsWrite {
	t.Helper()
	return model.SecurityNotificationSettingsWrite{ExpectedRevision: settings.Revision,
		EnabledEventKinds: settings.EnabledEventKinds, PrivacyLevel: settings.PrivacyLevel,
		LookbackDays: settings.LookbackDays, ThresholdFailures: settings.ThresholdFailures,
		ThresholdWindowSecond: settings.ThresholdWindowSecond, InvalidTOTPThreshold: settings.InvalidTOTPThreshold,
		RetentionDays: settings.RetentionDays}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
