package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
)

type auditPageEnvelope struct {
	Data model.AuditPage `json:"data"`
}

// newAuditPageHandler 引导一个 owner 会话并写入若干审计事件，供游标分页断言使用。
func newAuditPageHandler(t *testing.T) (*AdminAuthHandler, *model.AdminPrincipal, *adminauth.Service) {
	t.Helper()
	security := newHandlerSecurity(t)
	handler := NewAdminAuthHandler(security)
	bootstrap := callAdminHandler(t, nil, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"bootstrap","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, handler.HandleBootstrap)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d, %s", bootstrap.Code, bootstrap.Body.String())
	}
	principal, err := security.Authenticate(t.Context(), bootstrap.Result().Cookies()[0].Value)
	if err != nil {
		t.Fatalf("authenticate bootstrap session: %v", err)
	}
	for index := range 2 {
		if err := security.RecordServiceAudit(t.Context(), model.ServiceAuditEvent{
			Action: "service_token.request", ResourceType: "service_scope",
			ResourceID: "audit:read:" + string(rune('a'+index)), Outcome: model.AuditOutcomeSuccess,
		}); err != nil {
			t.Fatalf("record service audit %d: %v", index, err)
		}
	}
	return handler, &principal, security
}

func TestHandleAuditEventsPaginatesWithOpaqueCursors(t *testing.T) {
	handler, principal, _ := newAuditPageHandler(t)
	bootstrap := callAdminHandler(t, principal, http.MethodGet, "/api/v1/admin/audit-events?limit=2", "", handler.HandleAuditEvents)
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("first page = %d, %s", bootstrap.Code, bootstrap.Body.String())
	}
	var first auditPageEnvelope
	if err := json.Unmarshal(bootstrap.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(first.Data.Events) != 2 || first.Data.NextCursor == "" {
		t.Fatalf("expected two events plus a cursor, got %+v", first.Data)
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 3; page++ {
		path := "/api/v1/admin/audit-events?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		response := callAdminHandler(t, principal, http.MethodGet, path, "", handler.HandleAuditEvents)
		if response.Code != http.StatusOK {
			t.Fatalf("page %d = %d, %s", page, response.Code, response.Body.String())
		}
		var decoded auditPageEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}
		for _, event := range decoded.Data.Events {
			if seen[event.ID] {
				t.Fatalf("audit event %q repeated across cursor pages", event.ID)
			}
			seen[event.ID] = true
		}
		cursor = decoded.Data.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected every audit event exactly once, got %d", len(seen))
	}
}

func TestHandleAuditEventsRejectsInvalidQuery(t *testing.T) {
	handler, principal, _ := newAuditPageHandler(t)
	for _, path := range []string{
		"/api/v1/admin/audit-events?limit=0",
		"/api/v1/admin/audit-events?limit=201",
		"/api/v1/admin/audit-events?limit=abc",
		"/api/v1/admin/audit-events?offset=10",
		"/api/v1/admin/audit-events?cursor=!!!",
	} {
		response := callAdminHandler(t, principal, http.MethodGet, path, "", handler.HandleAuditEvents)
		want := http.StatusBadRequest
		if path == "/api/v1/admin/audit-events?offset=10" {
			// offset 不再是契约的一部分，未识别的参数被忽略而不是报错。
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("%s: status = %d, want %d", path, response.Code, want)
		}
	}
	if response := callAdminHandler(t, principal, http.MethodGet, "/api/v1/admin/audit-events",
		"", handler.HandleAuditEvents); response.Code != http.StatusOK {
		t.Fatalf("default page = %d, %s", response.Code, response.Body.String())
	}
}

func TestAuditEventFilterBoundariesAndServiceActor(t *testing.T) {
	request := newAdminRequest(t, nil, http.MethodGet, "/api/v1/admin/audit-events?limit=200&action=node.update&actor_id=abc", "")
	filter, err := auditEventFilter(request)
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	if filter.Limit != 200 || filter.Action != "node.update" || filter.ActorID != "abc" || filter.Cursor != "" {
		t.Fatalf("filter = %+v", filter)
	}
	if _, err := auditEventFilter(newAdminRequest(t, nil, http.MethodGet, "/api/v1/admin/audit-events?limit=201", "")); err == nil {
		t.Fatal("expected an out-of-range limit to be rejected")
	}
}
