package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestAdministratorAccountRoutesAtomicAudit(t *testing.T) {
	router := setupSessionRouter(t)
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", `{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatal(bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	body := `{"username":"operator","display_name":"Operator","role":"admin","password":"operator password long"}`
	denied := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users", body, cookie, "http://example.com")
	if denied.Code != http.StatusPreconditionRequired {
		t.Fatalf("creation without recent proof: %d", denied.Code)
	}
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal(reauth.Body.String())
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users", body, cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	var account struct {
		Data model.AdminUser `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"operator password long"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		t.Fatal(login.Body.String())
	}
	operator := login.Result().Cookies()[0]
	updated := requestRouterWithCookie(router, http.MethodPut, "/api/v1/admin/users/"+account.Data.ID, `{"display_name":"Operator owner","role":"owner","enabled":true}`, cookie, "http://example.com")
	if updated.Code != http.StatusOK {
		t.Fatal(updated.Body.String())
	}
	oldSession := requestRouterWithCookie(router, http.MethodGet, "/api/v1/auth/session", "", operator, "")
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("role change left session active: %d", oldSession.Code)
	}
	deleted := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/users/"+account.Data.ID, "", cookie, "http://example.com")
	if deleted.Code != http.StatusOK {
		t.Fatal(deleted.Body.String())
	}
	events := administratorAuditEvents(t, router, cookie)
	assertAdministratorSuccessAudits(t, events, account.Data.ID)
}

func assertAdministratorSuccessAudits(t *testing.T, events []model.AdminAuditEvent, targetID string) {
	t.Helper()
	for _, action := range []string{store.AdministratorCreate, store.AdministratorUpdate, store.AdministratorDelete} {
		count := 0
		for _, event := range events {
			if event.Action == action && event.Outcome == model.AuditOutcomeSuccess {
				count++
				for _, other := range events {
					if other.RequestID == event.RequestID && other.ResourceType == "http_route" && other.Outcome == model.AuditOutcomeSuccess {
						t.Fatal("duplicate generic success audit")
					}
				}
				if event.ResourceID != targetID || event.RequestID == "" {
					t.Fatalf("invalid event: %+v", event)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s success audits=%d", action, count)
		}
	}
}

func administratorAuditEvents(t *testing.T, router *Router, cookie *http.Cookie) []model.AdminAuditEvent {
	t.Helper()
	response := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/audit-events?limit=100", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Data model.AuditPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body.String(), "operator password long") || strings.Contains(response.Body.String(), "correct horse battery staple") || strings.Contains(response.Body.String(), "argon2id") {
		t.Fatal("audit disclosed credentials")
	}
	return result.Data.Events
}

func TestAdministratorPasswordRouteKeepsCurrentSessionAndOneAudit(t *testing.T) {
	router := setupSessionRouter(t)
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", `{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatal(bootstrap.Body.String())
	}
	current := bootstrap.Result().Cookies()[0]
	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login", `{"username":"owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		t.Fatal(login.Body.String())
	}
	other := login.Result().Cookies()[0]
	changed := requestRouterWithCookie(router, http.MethodPut, "/api/v1/admin/users/me/password", `{"current_password":"correct horse battery staple","new_password":"a different secure password"}`, current, "http://example.com")
	if changed.Code != http.StatusOK {
		t.Fatal(changed.Body.String())
	}
	for _, test := range []struct {
		cookie *http.Cookie
		status int
	}{{current, http.StatusOK}, {other, http.StatusUnauthorized}} {
		result := requestRouterWithCookie(router, http.MethodGet, "/api/v1/auth/session", "", test.cookie, "")
		if result.Code != test.status {
			t.Fatalf("session status %d, want %d", result.Code, test.status)
		}
	}
	count := 0
	for _, event := range administratorAuditEvents(t, router, current) {
		if event.Action == store.AdministratorPassword && event.Outcome == model.AuditOutcomeSuccess {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("password success audits=%d", count)
	}
}

func TestAdministratorAuditPolicy(t *testing.T) {
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "owner"}}
	for _, test := range []struct {
		pattern, action, id string
		atomic              bool
	}{
		{"POST /api/v1/admin/users", store.AdministratorCreate, "", true}, {"PUT /api/v1/admin/users/{id}", store.AdministratorUpdate, "target", true},
		{"DELETE /api/v1/admin/users/{id}", store.AdministratorDelete, "target", true}, {"PUT /api/v1/admin/users/me/password", store.AdministratorPassword, "owner", true}, {"GET /api/v1/admin/users", "", "", false},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Pattern = test.pattern
		request.SetPathValue("id", "target")
		action, id, atomic := administratorAuditPolicy(request, &principal)
		if action != test.action || id != test.id || atomic != test.atomic {
			t.Fatalf("policy: %s %s %v", action, id, atomic)
		}
	}
}
