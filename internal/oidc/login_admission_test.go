package oidc

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type loginHookTransport struct {
	base http.RoundTripper
	hook func() error
}

func (transport loginHookTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err == nil && request.URL.Path == "/token" {
		if hookErr := transport.hook(); hookErr != nil {
			_ = response.Body.Close()
			return nil, hookErr
		}
	}
	return response, err
}

func TestOIDCCallbackRejectsAuthorityChangesDuringExchange(t *testing.T) {
	for _, query := range []string{
		"UPDATE admin_oidc_providers SET enabled=0",
		"UPDATE admin_oidc_providers SET configuration_version=configuration_version+1",
		"UPDATE admin_users SET enabled=0",
		"DELETE FROM admin_external_identities",
	} {
		t.Run(strings.Fields(query)[1], func(t *testing.T) {
			service, identities, database, _, idp, user := setupOIDCService(t)
			provider := createTestProvider(t, service, idp.baseURL(), true)
			previous := time.Now().UTC().Add(-time.Hour)
			identity := model.ExternalIdentity{ID: "identity", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user", LastLoginAt: previous, CreatedAt: previous, UpdatedAt: previous}
			if err := identities.LinkIdentity(t.Context(), &identity); err != nil {
				t.Fatal(err)
			}
			state, binding := createLoginTransaction(t, service.secrets, identities, provider)
			idp.setAuthCode("code")
			service.client.client.Transport = loginHookTransport{base: service.client.client.Transport, hook: func() error { _, err := database.DB.Exec(query); return err }}
			path, token, err := service.HandleCallback(t.Context(), state, "code", binding, "127.0.0.1", "agent")
			if err == nil || path != "" || token != "" {
				t.Fatal("stale callback admitted", path, token, err)
			}
			assertNoOIDCCallbackAdmission(t, database.DB)
		})
	}
}

func TestOIDCCallbackAuditFailureLeavesNoSessionOrLoginTime(t *testing.T) {
	service, identities, database, _, idp, user := setupOIDCService(t)
	provider := createTestProvider(t, service, idp.baseURL(), true)
	previous := time.Now().UTC().Add(-time.Hour)
	identity := model.ExternalIdentity{ID: "identity", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user", LastLoginAt: previous, CreatedAt: previous, UpdatedAt: previous}
	if err := identities.LinkIdentity(t.Context(), &identity); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_login_audit BEFORE INSERT ON admin_audit_events WHEN NEW.action='auth.oidc.login' BEGIN SELECT RAISE(ABORT,'full'); END`); err != nil {
		t.Fatal(err)
	}
	state, binding := createLoginTransaction(t, service.secrets, identities, provider)
	idp.setAuthCode("code")
	path, token, err := service.HandleCallback(t.Context(), state, "code", binding, "127.0.0.1", "agent")
	if err == nil || path != "" || token != "" {
		t.Fatal("unaudited callback admitted", err)
	}
	assertNoOIDCCallbackAdmission(t, database.DB)
	restored, err := identities.GetIdentityByID(t.Context(), identity.ID)
	if err != nil || !restored.LastLoginAt.Equal(previous) {
		t.Fatal("partial identity login time", restored, err)
	}
}

func assertNoOIDCCallbackAdmission(t *testing.T, db *sql.DB) {
	t.Helper()
	var sessions, audits int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_sessions").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='auth.oidc.login'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	var login sql.NullTime
	if err := db.QueryRow("SELECT last_login_at FROM admin_users WHERE id='u-1'").Scan(&login); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || audits != 0 || login.Valid {
		t.Fatal("partial OIDC admission", sessions, audits, login)
	}
}
