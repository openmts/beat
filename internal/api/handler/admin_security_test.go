package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestAdminAuthHandlerFullLifecycle(t *testing.T) {
	security := newHandlerSecurity(t)
	handler := NewAdminAuthHandler(security)
	state := callAdminHandler(t, nil, http.MethodGet, "/api/v1/auth/state", "", handler.HandleState)
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"setup_required":true`) {
		t.Fatalf("state = %d, %s", state.Code, state.Body.String())
	}
	bootstrap := callAdminHandler(t, nil, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"bootstrap","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, handler.HandleBootstrap)
	if bootstrap.Code != http.StatusCreated || len(bootstrap.Result().Cookies()) != 1 {
		t.Fatalf("bootstrap = %d, %s", bootstrap.Code, bootstrap.Body.String())
	}
	rawToken := bootstrap.Result().Cookies()[0].Value
	principal, err := security.Authenticate(t.Context(), rawToken)
	if err != nil {
		t.Fatalf("authenticate bootstrap session: %v", err)
	}
	session := callAdminHandler(t, &principal, http.MethodGet, "/api/v1/auth/session", "", handler.HandleSession)
	if session.Code != http.StatusOK {
		t.Fatalf("session = %d", session.Code)
	}
	reauth := callAdminHandler(t, &principal, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, handler.HandleReauthenticate)
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate = %d, %s", reauth.Code, reauth.Body.String())
	}
	principal, err = security.Authenticate(t.Context(), rawToken)
	if err != nil {
		t.Fatalf("refresh reauthenticated principal: %v", err)
	}

	created := callAdminHandler(t, &principal, http.MethodPost, "/api/v1/admin/users",
		`{"username":"operator","display_name":"Operator","role":"admin",`+
			`"password":"operator password long"}`, handler.HandleCreateUser)
	var createdBody struct {
		Data model.AdminUser `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil || createdBody.Data.ID == "" {
		t.Fatalf("create administrator = %d, %s, %v", created.Code, created.Body.String(), err)
	}
	listed := callAdminHandler(t, &principal, http.MethodGet, "/api/v1/admin/users", "", handler.HandleListUsers)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "operator") {
		t.Fatalf("list administrators = %d, %s", listed.Code, listed.Body.String())
	}
	updatedRequest := newAdminRequest(t, &principal, http.MethodPut, "/api/v1/admin/users/id",
		`{"display_name":"Updated","role":"admin","enabled":true}`)
	updatedRequest.SetPathValue("id", createdBody.Data.ID)
	updated := httptest.NewRecorder()
	handler.HandleUpdateUser(updated, updatedRequest)
	if updated.Code != http.StatusOK {
		t.Fatalf("update administrator = %d, %s", updated.Code, updated.Body.String())
	}

	setup := callAdminHandler(t, &principal, http.MethodPost, "/api/v1/admin/users/me/totp",
		`{"code":""}`, handler.HandleTOTP)
	var setupBody struct {
		Data adminauth.TOTPSetup `json:"data"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &setupBody); err != nil || setupBody.Data.Secret == "" {
		t.Fatalf("TOTP setup = %d, %s, %v", setup.Code, setup.Body.String(), err)
	}
	code, err := totp.GenerateCode(setupBody.Data.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}
	enabled := callAdminHandler(t, &principal, http.MethodPost, "/api/v1/admin/users/me/totp",
		`{"code":"`+code+`"}`, handler.HandleTOTP)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable TOTP = %d, %s", enabled.Code, enabled.Body.String())
	}
	code, _ = totp.GenerateCode(setupBody.Data.Secret, time.Now().UTC())
	reauth = callAdminHandler(t, &principal, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":"`+code+`"}`,
		handler.HandleReauthenticate)
	if reauth.Code != http.StatusOK {
		t.Fatalf("TOTP reauthenticate = %d, %s", reauth.Code, reauth.Body.String())
	}
	principal, err = security.Authenticate(t.Context(), rawToken)
	if err != nil {
		t.Fatalf("refresh TOTP principal: %v", err)
	}
	disabled := callAdminHandler(t, &principal, http.MethodDelete, "/api/v1/admin/users/me/totp", "",
		handler.HandleDisableTOTP)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable TOTP = %d, %s", disabled.Code, disabled.Body.String())
	}

	sessions := callAdminHandler(t, &principal, http.MethodGet, "/api/v1/admin/sessions", "",
		handler.HandleListSessions)
	if sessions.Code != http.StatusOK {
		t.Fatalf("list sessions = %d", sessions.Code)
	}
	revokeOthers := callAdminHandler(t, &principal, http.MethodDelete, "/api/v1/admin/sessions/others", "",
		handler.HandleRevokeOtherSessions)
	if revokeOthers.Code != http.StatusOK {
		t.Fatalf("revoke other sessions = %d", revokeOthers.Code)
	}
	missingRevokeRequest := newAdminRequest(t, &principal, http.MethodDelete, "/api/v1/admin/sessions/missing", "")
	missingRevokeRequest.SetPathValue("id", "missing")
	missingRevoke := httptest.NewRecorder()
	handler.HandleRevokeSession(missingRevoke, missingRevokeRequest)
	if missingRevoke.Code != http.StatusNotFound {
		t.Fatalf("missing session revoke = %d", missingRevoke.Code)
	}

	audit := callAdminHandler(t, &principal, http.MethodGet, "/api/v1/admin/audit-events?limit=10", "",
		handler.HandleAuditEvents)
	if audit.Code != http.StatusOK {
		t.Fatalf("audit = %d, %s", audit.Code, audit.Body.String())
	}
	changed := callAdminHandler(t, &principal, http.MethodPut, "/api/v1/admin/users/me/password",
		`{"current_password":"correct horse battery staple",`+
			`"new_password":"replacement horse battery staple","totp_code":""}`,
		handler.HandleChangePassword)
	if changed.Code != http.StatusOK {
		t.Fatalf("change password = %d, %s", changed.Code, changed.Body.String())
	}
	deleteRequest := newAdminRequest(t, &principal, http.MethodDelete, "/api/v1/admin/users/id", "")
	deleteRequest.SetPathValue("id", createdBody.Data.ID)
	deleted := httptest.NewRecorder()
	handler.HandleDeleteUser(deleted, deleteRequest)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete administrator = %d, %s", deleted.Code, deleted.Body.String())
	}
	logout := callAdminHandler(t, &principal, http.MethodPost, "/api/v1/auth/logout", "", handler.HandleLogout)
	if logout.Code != http.StatusOK || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout = %d, %#v", logout.Code, logout.Result().Cookies())
	}
}

func TestAdminAuthHandlerRejectsMalformedAndUnauthenticatedRequests(t *testing.T) {
	handler := NewAdminAuthHandler(newHandlerSecurity(t))
	for _, call := range []func(http.ResponseWriter, *http.Request){handler.HandleSession,
		handler.HandleListUsers, handler.HandleCreateUser, handler.HandleUpdateUser,
		handler.HandleDeleteUser, handler.HandleChangePassword, handler.HandleTOTP,
		handler.HandleDisableTOTP, handler.HandleListSessions, handler.HandleRevokeSession,
		handler.HandleRevokeOtherSessions, handler.HandleAuditEvents, handler.HandleLogout,
		handler.HandleReauthenticate} {
		response := callAdminHandler(t, nil, http.MethodGet, "/", "", call)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated status = %d", response.Code)
		}
	}
	principal := backupPrincipal(model.AdminRoleOwner, true)
	malformedCalls := []struct {
		principal *model.AdminPrincipal
		call      func(http.ResponseWriter, *http.Request)
	}{
		{call: handler.HandleBootstrap},
		{call: handler.HandleLogin},
		{principal: principal, call: handler.HandleReauthenticate},
		{principal: principal, call: handler.HandleCreateUser},
		{principal: principal, call: handler.HandleUpdateUser},
		{principal: principal, call: handler.HandleChangePassword},
		{principal: principal, call: handler.HandleTOTP},
	}
	for _, test := range malformedCalls {
		malformed := callAdminHandler(t, test.principal, http.MethodPost, "/", `{`, test.call)
		if malformed.Code != http.StatusBadRequest {
			t.Fatalf("malformed request status = %d", malformed.Code)
		}
	}
	trailing := callAdminHandler(t, nil, http.MethodPost, "/api/v1/auth/login",
		`{"username":"owner","password":"password"} {}`, handler.HandleLogin)
	if trailing.Code != http.StatusBadRequest {
		t.Fatalf("trailing login data status = %d", trailing.Code)
	}
}

func TestAdminAuthHandlerStorageFailures(t *testing.T) {
	handler := NewAdminAuthHandler(newClosedHandlerSecurity(t))
	recentUntil := time.Now().UTC().Add(time.Hour)
	principal := &model.AdminPrincipal{
		User:    model.AdminUser{ID: "owner", Username: "owner", Role: model.AdminRoleOwner, Enabled: true},
		Session: model.AdminSession{ID: "session", UserID: "owner", ReauthenticatedUntil: &recentUntil},
	}
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		status int
	}{
		{name: "state", method: http.MethodGet, call: handler.HandleState, status: http.StatusInternalServerError},
		{name: "bootstrap", method: http.MethodPost, body: `{"bootstrap_token":"bootstrap","username":"owner",` +
			`"display_name":"Owner","password":"correct horse battery staple"}`,
			call: handler.HandleBootstrap, status: http.StatusBadRequest},
		{name: "login", method: http.MethodPost, body: `{"username":"owner","password":"password"}`,
			call: handler.HandleLogin, status: http.StatusBadRequest},
		{name: "reauthenticate", method: http.MethodPost,
			body: `{"password":"correct horse battery staple","totp_code":""}`,
			call: handler.HandleReauthenticate, status: http.StatusUnauthorized},
		{name: "logout", method: http.MethodPost, call: handler.HandleLogout, status: http.StatusInternalServerError},
		{name: "list users", method: http.MethodGet, call: handler.HandleListUsers, status: http.StatusBadRequest},
		{name: "create user", method: http.MethodPost,
			body: `{"username":"operator","display_name":"Operator","role":"admin",` +
				`"password":"operator password long"}`,
			call: handler.HandleCreateUser, status: http.StatusBadRequest},
		{name: "update user", method: http.MethodPut, body: `{"display_name":"Operator","role":"admin","enabled":true}`,
			call: handler.HandleUpdateUser, status: http.StatusBadRequest},
		{name: "delete user", method: http.MethodDelete, call: handler.HandleDeleteUser, status: http.StatusBadRequest},
		{name: "change password", method: http.MethodPut,
			body: `{"current_password":"current password long","new_password":"replacement password long"}`,
			call: handler.HandleChangePassword, status: http.StatusUnauthorized},
		{name: "begin TOTP", method: http.MethodPost, body: `{"code":""}`,
			call: handler.HandleTOTP, status: http.StatusBadRequest},
		{name: "enable TOTP", method: http.MethodPost, body: `{"code":"000000"}`,
			call: handler.HandleTOTP, status: http.StatusBadRequest},
		{name: "disable TOTP", method: http.MethodDelete, call: handler.HandleDisableTOTP, status: http.StatusBadRequest},
		{name: "list sessions", method: http.MethodGet, call: handler.HandleListSessions, status: http.StatusBadRequest},
		{name: "revoke session", method: http.MethodDelete, call: handler.HandleRevokeSession, status: http.StatusBadRequest},
		{name: "revoke others", method: http.MethodDelete, call: handler.HandleRevokeOtherSessions,
			status: http.StatusBadRequest},
		{name: "audit events", method: http.MethodGet, path: "/?limit=invalid&offset=invalid",
			call: handler.HandleAuditEvents, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path == "" {
				path = "/"
			}
			request := newAdminRequest(t, principal, test.method, path, test.body)
			request.SetPathValue("id", "other")
			response := httptest.NewRecorder()
			test.call(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestAdministratorErrorMappingsAndHelpers(t *testing.T) {
	authenticationErrors := []struct {
		err    error
		status int
	}{
		{err: adminauth.ErrLocked, status: http.StatusTooManyRequests},
		{err: adminauth.ErrBootstrapComplete, status: http.StatusConflict},
		{err: adminauth.ErrTOTPRequired, status: http.StatusPreconditionRequired},
		{err: adminauth.ErrBootstrapDenied, status: http.StatusUnauthorized},
		{err: adminauth.ErrInvalidTOTP, status: http.StatusUnauthorized},
	}
	for _, test := range authenticationErrors {
		response := httptest.NewRecorder()
		writeAuthenticationError(response, test.err)
		if response.Code != test.status {
			t.Fatalf("authentication error %v status = %d, want %d", test.err, response.Code, test.status)
		}
	}
	securityErrors := []struct {
		err    error
		status int
	}{
		{err: adminauth.ErrForbidden, status: http.StatusForbidden},
		{err: adminauth.ErrRecentAuthRequired, status: http.StatusPreconditionRequired},
		{err: store.ErrLastOwner, status: http.StatusConflict},
		{err: store.ErrAdminUserNotFound, status: http.StatusNotFound},
		{err: store.ErrAdminSessionNotFound, status: http.StatusNotFound},
		{err: adminauth.ErrInvalidCredentials, status: http.StatusUnauthorized},
	}
	for _, test := range securityErrors {
		response := httptest.NewRecorder()
		writeSecurityError(response, test.err)
		if response.Code != test.status {
			t.Fatalf("security error %v status = %d, want %d", test.err, response.Code, test.status)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/?empty=", nil)
	request.RemoteAddr = "local-address"
	if remoteIP(request) != "local-address" || boundedQueryInt(request, "empty", 7) != 7 {
		t.Fatal("request helper fallback values are incorrect")
	}
}
