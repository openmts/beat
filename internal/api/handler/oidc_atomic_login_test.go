package handler

import (
	"crypto/sha256"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestOIDCCallbackAuditFailureNeverSetsSessionCookie(t *testing.T) {
	handler, request, database := atomicOIDCHandlerFixture(t)
	if _, err := database.Exec(`CREATE TRIGGER fail_oidc_audit BEFORE INSERT ON admin_audit_events WHEN NEW.action='auth.oidc.login' BEGIN SELECT RAISE(ABORT,'full'); END`); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.HandleCallback(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") == "/admin" {
		t.Fatal("failed admission was successful", response.Code, response.Header())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "beat_admin_session" && cookie.Value != "" {
			t.Fatal("failed admission set session cookie")
		}
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM admin_sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan session", count, err)
	}
}

func atomicOIDCHandlerFixture(t *testing.T) (*OIDCHandler, *http.Request, *sql.DB) {
	t.Helper()
	database, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	admins := store.NewAdminStore(database.DB)
	now := time.Now().UTC()
	user := model.AdminUser{ID: "atomic-user", Username: "atomic-user", DisplayName: "OIDC", Role: model.AdminRoleAdmin, PasswordHash: "hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	auth, err := adminauth.NewService(adminauth.ServiceConfig{Store: admins, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	idp := newHandlerTestIdP(t)
	client := oidc.NewProviderClient(oidc.AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(idp.server.Client().Transport.(*http.Transport).TLSClientConfig)
	identities := store.NewOIDCStore(database.DB)
	handler := NewOIDCHandler(oidc.NewService(identities, admins, secrets, auth, client, time.Now, nil))
	provider := createHandlerProvider(t, handler, idp)
	if err := identities.LinkIdentity(t.Context(), &model.ExternalIdentity{ID: "atomic-identity", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user", LastLoginAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	stateHash, bindingHash, nonceHash := sha256.Sum256([]byte("atomic-state")), sha256.Sum256([]byte("atomic-binding")), sha256.Sum256([]byte("nonce-test"))
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("verifier"), AAD: oidc.TransactionVerifierAAD("atomic-transaction", provider.ID), Persist: func(ciphertext []byte) error {
		return identities.CreateTransaction(t.Context(), &model.OIDCTransaction{ID: "atomic-transaction", StateHash: stateHash[:], BrowserBindingHash: bindingHash[:], NonceHash: nonceHash[:], CodeVerifierEncrypted: ciphertext, ProviderID: provider.ID, ProviderConfigurationVersion: provider.ConfigurationVersion, Purpose: model.OIDCPurposeLogin, RedirectURI: "http://example.com/api/v1/auth/oidc/callback", ReturnPath: "/admin", CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	}})
	idp.setAuthCode("atomic-code")
	request := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/auth/oidc/callback?state=atomic-state&code=atomic-code", nil)
	request.AddCookie(&http.Cookie{Name: oidcBrowserBindingCookie, Value: "atomic-binding"})
	return handler, request, database.DB
}
