package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type selfRouteFixture struct {
	router *Router
	cookie *http.Cookie
	target string
}

func newSelfRouteFixture(t *testing.T) selfRouteFixture {
	t.Helper()
	router := setupSessionRouter(t)
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", `{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatal(bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal(reauth.Body.String())
	}
	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login", `{"username":"owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		t.Fatal(login.Body.String())
	}
	session := requestRouterWithCookie(router, http.MethodGet, "/api/v1/auth/session", "", login.Result().Cookies()[0], "")
	var principal struct {
		Data model.AdminPrincipal `json:"data"`
	}
	if err := json.Unmarshal(session.Body.Bytes(), &principal); err != nil {
		t.Fatal(err)
	}
	return selfRouteFixture{router: router, cookie: cookie, target: principal.Data.Session.ID}
}

func TestSelfSecurityRoutesHaveSingleConcreteAudit(t *testing.T) {
	f := newSelfRouteFixture(t)
	revoked := f.request(http.MethodDelete, "/api/v1/admin/sessions/"+f.target, "")
	if revoked.Code != http.StatusOK {
		t.Fatal(revoked.Body.String())
	}
	others := f.request(http.MethodDelete, "/api/v1/admin/sessions/others", "")
	if others.Code != http.StatusOK {
		t.Fatal(others.Body.String())
	}
	setup := f.request(http.MethodPost, "/api/v1/admin/users/me/totp", `{"code":""}`)
	if setup.Code != http.StatusOK {
		t.Fatal(setup.Body.String())
	}
	var pending struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(pending.Data.Secret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	enabled := f.request(http.MethodPost, "/api/v1/admin/users/me/totp", `{"code":"`+code+`"}`)
	if enabled.Code != http.StatusOK {
		t.Fatal(enabled.Body.String())
	}
	disabled := f.request(http.MethodDelete, "/api/v1/admin/users/me/totp", "")
	if disabled.Code != http.StatusOK {
		t.Fatal(disabled.Body.String())
	}
	events := administratorAuditEvents(t, f.router, f.cookie)
	for _, event := range events {
		if strings.Contains(event.DetailJSON, pending.Data.Secret) {
			t.Fatal("audit disclosed TOTP secret")
		}
	}
	assertSelfRouteAuditCounts(t, events, map[string]string{store.AdministratorSessionRevoke: revoked.Header().Get("X-Request-ID"), store.AdministratorSessionsRevoke: others.Header().Get("X-Request-ID"), store.AdministratorTOTPBegin: setup.Header().Get("X-Request-ID"), store.AdministratorTOTPEnable: enabled.Header().Get("X-Request-ID"), store.AdministratorTOTPDisable: disabled.Header().Get("X-Request-ID")})
}

func assertSelfRouteAuditCounts(t *testing.T, events []model.AdminAuditEvent, requests map[string]string) {
	t.Helper()
	for action, requestID := range requests {
		count := 0
		for _, event := range events {
			if event.Action == action && event.Outcome == model.AuditOutcomeSuccess {
				count++
				if event.RequestID != requestID {
					t.Fatal("audit request ID diverged")
				}
			}
			if event.RequestID == requestID && event.ResourceType == "http_route" {
				t.Fatal("duplicate generic audit")
			}
		}
		if count != 1 {
			t.Fatalf("%s has %d success audits", action, count)
		}
	}
}

func (f selfRouteFixture) request(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(f.cookie)
	request.Header.Set("Origin", "http://example.com")
	response := httptest.NewRecorder()
	middleware.RequestContext(middleware.TrustedProxies{})(f.router.ServeHandler()).ServeHTTP(response, request)
	return response
}
