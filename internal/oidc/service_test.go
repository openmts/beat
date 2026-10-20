package oidc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func setupOIDCService(t *testing.T) (*Service, *store.OIDCStore, *store.SQLiteStore, *applicationsecrets.Service, *testOIDCProvider, *model.AdminUser) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	secrets := testutil.NewSecrets(t, sqlite.DB)
	oidcStore := store.NewOIDCStore(sqlite.DB)
	adminStore := store.NewAdminStore(sqlite.DB)

	now := time.Now().UTC()
	user := &model.AdminUser{
		ID: "u-1", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner,
		PasswordHash: "hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := adminStore.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	auth, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: adminStore, Secrets: secrets, LegacyToken: "bootstrap",
	})
	if err != nil {
		t.Fatalf("create adminauth service: %v", err)
	}

	provider := newTestOIDCProvider(t)
	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(provider.server.Client().Transport.(*http.Transport).TLSClientConfig)

	service := NewService(oidcStore, adminStore, secrets, auth, client, time.Now, nil)
	return service, oidcStore, sqlite, secrets, provider, user
}

func createTestProvider(t *testing.T, service *Service, issuer string, enabled bool) *model.OIDCProvider {
	t.Helper()
	provider, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "test-" + fmt.Sprintf("%d", time.Now().UnixNano()), DisplayName: "Test IdP", IssuerURL: issuer, ClientID: "client-1",
		ClientSecret: "secret-1", Scopes: []string{"openid", "profile", "email"},
		Enabled: enabled, SortOrder: 1,
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

// createLoginTransaction 直接构造一个登录事务，便于精确控制 nonce/binding。
func createLoginTransaction(
	t *testing.T, secrets secretbox.ResourceService, oidc *store.OIDCStore, provider *model.OIDCProvider,
) (state, binding string) {
	t.Helper()
	state = "state-controlled"
	binding = "binding-controlled"
	verifier := "verifier-controlled-value"
	transactionID := "txn-1"
	encrypted, err := sealOIDCTestResource(secrets, []byte(verifier), secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: transactionID,
		Field: "pkce_verifier", Revision: provider.ID, PayloadVersion: "v1",
	})
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	now := time.Now().UTC()
	transaction := &model.OIDCTransaction{
		ID: transactionID, StateHash: sha256sum([]byte(state)),
		BrowserBindingHash: sha256sum([]byte(binding)), NonceHash: sha256sum([]byte("nonce-test")),
		CodeVerifierEncrypted: encrypted, ProviderID: provider.ID,
		ProviderConfigurationVersion: provider.ConfigurationVersion,
		Purpose:                      model.OIDCPurposeLogin, RedirectURI: "https://beat.test/api/v1/auth/oidc/callback",
		ReturnPath: "/admin", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := oidc.CreateTransaction(context.Background(), transaction); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	return state, binding
}

func sha256sum(raw []byte) []byte {
	digest := sha256.Sum256(raw)
	return digest[:]
}

func TestCreateProviderValidatesDiscovery(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)

	created, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "ok", DisplayName: "OK IdP", IssuerURL: providerServer.baseURL(),
		ClientID: "client-1", ClientSecret: "secret-1",
		Scopes: []string{"openid"}, Enabled: true,
	}, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if created.IssuerURL != providerServer.baseURL() {
		t.Fatalf("issuer = %q", created.IssuerURL)
	}
	if !created.ClientSecretConfigured() {
		t.Fatal("client secret should be configured")
	}

	// 无效 issuer：非 https
	if _, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "bad", DisplayName: "Bad", IssuerURL: "http://issuer.test", ClientID: "c",
		ClientSecret: "s", Scopes: []string{"openid"},
	}, "127.0.0.1", "agent"); !errors.Is(err, ErrInvalidProviderInput) {
		t.Fatalf("http issuer error = %v", err)
	}
	// 缺 openid scope
	if _, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "bad2", DisplayName: "Bad", IssuerURL: providerServer.baseURL(), ClientID: "c",
		ClientSecret: "s", Scopes: []string{"profile"},
	}, "127.0.0.1", "agent"); !errors.Is(err, ErrInvalidProviderInput) {
		t.Fatalf("scope error = %v", err)
	}
	// 缺密钥
	if _, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "bad3", DisplayName: "Bad", IssuerURL: providerServer.baseURL(), ClientID: "c",
		ClientSecret: "", Scopes: []string{"openid"},
	}, "127.0.0.1", "agent"); !errors.Is(err, ErrInvalidProviderInput) {
		t.Fatalf("secret error = %v", err)
	}
	// 无法发现的 issuer
	if _, err := service.CreateProvider(context.Background(), ProviderInput{
		Slug: "bad4", DisplayName: "Bad", IssuerURL: "https://unreachable.invalid",
		ClientID: "c", ClientSecret: "s", Scopes: []string{"openid"},
	}, "127.0.0.1", "agent"); err == nil {
		t.Fatal("unreachable issuer must fail creation")
	}
}

func TestUpdateProviderIssuerImmutable(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	created := createTestProvider(t, service, providerServer.baseURL(), true)

	_, err := service.UpdateProvider(context.Background(), created.ID, ProviderInput{
		DisplayName: "Renamed", IssuerURL: "https://other.invalid", ClientID: created.ClientID,
		Scopes: created.Scopes, Enabled: true,
	}, "127.0.0.1", "agent")
	if !errors.Is(err, ErrIssuerImmutable) {
		t.Fatalf("immutable issuer error = %v", err)
	}

	updated, err := service.UpdateProvider(context.Background(), created.ID, ProviderInput{
		DisplayName: "Renamed", ClientID: created.ClientID, Scopes: created.Scopes,
		Enabled: false,
	}, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("update provider: %v", err)
	}
	if updated.Enabled {
		t.Fatal("provider should be disabled")
	}
	if updated.ConfigurationVersion != created.ConfigurationVersion {
		t.Fatalf("version changed without secret rotation: %d", updated.ConfigurationVersion)
	}
}

func TestUpdateProviderSecretRotationBumpsVersion(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	created := createTestProvider(t, service, providerServer.baseURL(), true)

	updated, err := service.UpdateProvider(context.Background(), created.ID, ProviderInput{
		DisplayName: created.DisplayName, ClientID: created.ClientID,
		ClientSecret: "new-secret", Scopes: created.Scopes, Enabled: true,
	}, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("rotate secret: %v", err)
	}
	if updated.ConfigurationVersion != created.ConfigurationVersion+1 {
		t.Fatalf("version = %d, want %d", updated.ConfigurationVersion, created.ConfigurationVersion+1)
	}
	// 新版本 AAD 可解密，旧版本 AAD 必须失败
	plaintext, err := service.secrets.OpenResource(context.Background(), updated.ClientSecretEncrypted,
		ProviderSecretAAD(updated.ID, updated.ConfigurationVersion))
	if err != nil || string(plaintext) != "new-secret" {
		t.Fatalf("decrypt rotated secret: %v / %q", err, plaintext)
	}
	if _, err := service.secrets.OpenResource(context.Background(), updated.ClientSecretEncrypted,
		ProviderSecretAAD(updated.ID, created.ConfigurationVersion)); err == nil {
		t.Fatal("old-version AAD must fail to decrypt rotated secret")
	}
}

func TestDeleteProviderRejectedWithIdentities(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	now := time.Now().UTC()
	if err := oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	if err := service.DeleteProvider(context.Background(), provider.ID, "127.0.0.1", "agent"); !errors.Is(err, ErrProviderInUse) {
		t.Fatalf("delete with identities = %v", err)
	}
}

func TestBeginLoginBuildsAuthorizationURL(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	result, err := service.BeginLogin(context.Background(), provider.ID, "/admin",
		"https://beat.test", "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	for _, fragment := range []string{
		"response_type=code", "client_id=client-1",
		"redirect_uri=https%3A%2F%2Fbeat.test%2Fapi%2Fv1%2Fauth%2Foidc%2Fcallback",
		"code_challenge=", "code_challenge_method=S256", "state=", "nonce=",
	} {
		if !strings.Contains(result.AuthorizationURL, fragment) {
			t.Fatalf("authorization url missing %q: %s", fragment, result.AuthorizationURL)
		}
	}
	if result.BrowserBinding.Path != "/api/v1/auth/oidc/callback" || result.BrowserBinding.Value == "" {
		t.Fatalf("browser binding = %#v", result.BrowserBinding)
	}
}

func TestBeginLoginRequiresEnabledProvider(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	disabled := createTestProvider(t, service, providerServer.baseURL(), false)

	if _, err := service.BeginLogin(context.Background(), disabled.ID, "/admin",
		"https://beat.test", "127.0.0.1", "agent"); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("disabled provider error = %v", err)
	}
	if _, err := service.BeginLogin(context.Background(), "missing", "/admin",
		"https://beat.test", "127.0.0.1", "agent"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("missing provider error = %v", err)
	}
}

func TestLoginCallbackSuccess(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	now := time.Now().UTC()
	if err := oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		Email: "user@example.com", EmailVerified: true, LastLoginAt: now,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	state, binding := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("code-1")

	returnPath, token, err := service.HandleCallback(context.Background(),
		state, "code-1", binding, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if returnPath != "/admin" || token == "" {
		t.Fatalf("callback result = %q / token-len=%d", returnPath, len(token))
	}
}

func TestLoginCallbackUnlinkedIdentityFails(t *testing.T) {
	service, oidc, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	state, binding := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("code-unlinked")

	_, _, err := service.HandleCallback(context.Background(), state, "code-unlinked", binding, "127.0.0.1", "agent")
	if !errors.Is(err, ErrIdentityNotLinked) {
		t.Fatalf("unlinked identity error = %v", err)
	}
}

func TestLoginCallbackRejectsBadBrowserBinding(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	_ = oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	})

	state, _ := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("code-1")

	if _, _, err := service.HandleCallback(context.Background(), state, "code-1",
		"wrong-binding", "127.0.0.1", "agent"); !errors.Is(err, ErrCallbackFailed) {
		t.Fatalf("bad binding error = %v", err)
	}
}

func TestLoginCallbackConsumedTransactionFails(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	_ = oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	})

	state, binding := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("code-1")
	if _, _, err := service.HandleCallback(context.Background(), state, "code-1", binding, "127.0.0.1", "agent"); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	providerServer.setAuthCode("code-2")
	if _, _, err := service.HandleCallback(context.Background(), state, "code-2", binding, "127.0.0.1", "agent"); !errors.Is(err, ErrCallbackFailed) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestLinkBeginRequiresRecentAuthentication(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	principal := &model.AdminPrincipal{User: model.AdminUser{ID: "u-1"}}
	if _, err := service.BeginLink(context.Background(), principal, provider.ID, "/admin",
		"https://beat.test", "127.0.0.1", "agent"); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("recent auth error = %v", err)
	}
}

func TestLinkCallbackSuccess(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	// 链接事务指向用户
	state := "link-state"
	binding := "link-binding"
	transactionID := "txn-link"
	encrypted, err := sealOIDCTestResource(service.secrets, []byte("verifier"), secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: transactionID,
		Field: "pkce_verifier", Revision: provider.ID, PayloadVersion: "v1",
	})
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	now := time.Now().UTC()
	if err := oidc.CreateTransaction(context.Background(), &model.OIDCTransaction{
		ID: transactionID, StateHash: sha256sum([]byte(state)),
		BrowserBindingHash: sha256sum([]byte(binding)), NonceHash: sha256sum([]byte("nonce-test")),
		CodeVerifierEncrypted: encrypted, ProviderID: provider.ID,
		ProviderConfigurationVersion: provider.ConfigurationVersion,
		Purpose:                      model.OIDCPurposeLink, UserID: user.ID,
		RedirectURI: "https://beat.test/api/v1/auth/oidc/callback", ReturnPath: "/admin/security",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create link transaction: %v", err)
	}
	providerServer.setAuthCode("link-code")

	returnPath, token, err := service.HandleCallback(context.Background(), state, "link-code", binding, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("link callback: %v", err)
	}
	if returnPath != "/admin/security" || token != "" {
		t.Fatalf("link result = %q / %q", returnPath, token)
	}

	identities, err := service.ListIdentitiesForUser(context.Background(), user.ID)
	if err != nil || len(identities) != 1 {
		t.Fatalf("linked identities = %v / %v", identities, err)
	}
	if identities[0].ProviderName != "Test IdP" || identities[0].Subject != "sub-test-user" {
		t.Fatalf("identity = %#v", identities[0])
	}
}

func TestUnlinkRequiresRecentAuthAndOwnership(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	if err := oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	principal := &model.AdminPrincipal{User: model.AdminUser{ID: user.ID}}
	if err := service.Unlink(context.Background(), principal, "id-1", "127.0.0.1", "agent"); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("unlink without recent auth = %v", err)
	}

	recent := now.Add(time.Minute)
	principal.Session = model.AdminSession{ReauthenticatedUntil: &recent}
	if err := service.Unlink(context.Background(), principal, "missing", "127.0.0.1", "agent"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("unlink missing = %v", err)
	}

	other := &model.AdminPrincipal{
		User:    model.AdminUser{ID: "other-user"},
		Session: model.AdminSession{ReauthenticatedUntil: &recent},
	}
	if err := service.Unlink(context.Background(), other, "id-1", "127.0.0.1", "agent"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("unlink other user = %v", err)
	}

	if err := service.Unlink(context.Background(), principal, "id-1", "127.0.0.1", "agent"); err != nil {
		t.Fatalf("unlink own identity: %v", err)
	}
	left, err := service.ListIdentitiesForUser(context.Background(), user.ID)
	if err != nil || len(left) != 0 {
		t.Fatalf("identities after unlink = %v / %v", left, err)
	}
}

func TestListPublicProvidersOnlyEnabled(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	createTestProvider(t, service, providerServer.baseURL(), true)
	secondProvider := newTestOIDCProvider(t)
	createTestProvider(t, service, secondProvider.baseURL(), false)

	public, err := service.ListPublicProviders(context.Background())
	if err != nil {
		t.Fatalf("list public providers: %v", err)
	}
	if len(public) != 1 {
		t.Fatalf("public providers = %d, want 1", len(public))
	}
	if public[0].DisplayName == "" || public[0].ID == "" {
		t.Fatalf("public provider = %#v", public[0])
	}
}

func TestValidateReturnPathRejectsExternal(t *testing.T) {
	for _, path := range []string{"", "/evil", "https://evil.test", "/admin/../escape", "/api/v1/other"} {
		if err := validateReturnPath(path); err == nil {
			t.Fatalf("return path %q accepted", path)
		}
	}
	for _, path := range []string{"/admin", "/admin/security", "/admin/nodes"} {
		if err := validateReturnPath(path); err != nil {
			t.Fatalf("return path %q rejected: %v", path, err)
		}
	}
}

func TestBeginLinkSuccessWithRecentAuthentication(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	recent := time.Now().UTC().Add(time.Minute)
	principal := &model.AdminPrincipal{
		User:    model.AdminUser{ID: "u-1"},
		Session: model.AdminSession{ReauthenticatedUntil: &recent},
	}
	result, err := service.BeginLink(context.Background(), principal, provider.ID, "/admin/security",
		"https://beat.test", "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("begin link: %v", err)
	}
	if !strings.Contains(result.AuthorizationURL, "state=") {
		t.Fatalf("authorization url = %q", result.AuthorizationURL)
	}
}

func TestLoginCallbackNonceMismatchFails(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	_ = oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	})

	// 用错误的 nonce 哈希构造事务
	state := "state-wrong-nonce"
	binding := "binding-wrong-nonce"
	encrypted, err := sealOIDCTestResource(service.secrets, []byte("verifier"), secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: "txn-nonce",
		Field: "pkce_verifier", Revision: provider.ID, PayloadVersion: "v1",
	})
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	if err := oidc.CreateTransaction(context.Background(), &model.OIDCTransaction{
		ID: "txn-nonce", StateHash: sha256sum([]byte(state)),
		BrowserBindingHash: sha256sum([]byte(binding)), NonceHash: sha256sum([]byte("different-nonce")),
		CodeVerifierEncrypted: encrypted, ProviderID: provider.ID,
		ProviderConfigurationVersion: provider.ConfigurationVersion, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.test/api/v1/auth/oidc/callback", ReturnPath: "/admin",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	providerServer.setAuthCode("code-nonce")

	_, _, err = service.HandleCallback(context.Background(), state, "code-nonce", binding, "127.0.0.1", "agent")
	if err == nil || !strings.Contains(err.Error(), "nonce mismatch") {
		t.Fatalf("nonce mismatch error = %v", err)
	}
}

func TestLoginCallbackProviderVersionMismatchFails(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	_ = oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	})

	state := "state-version"
	binding := "binding-version"
	encrypted, err := sealOIDCTestResource(service.secrets, []byte("verifier"), secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: "txn-version",
		Field: "pkce_verifier", Revision: provider.ID, PayloadVersion: "v1",
	})
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	// 事务记录版本 99，与提供方当前版本 1 不一致
	if err := oidc.CreateTransaction(context.Background(), &model.OIDCTransaction{
		ID: "txn-version", StateHash: sha256sum([]byte(state)),
		BrowserBindingHash: sha256sum([]byte(binding)), NonceHash: sha256sum([]byte("nonce-test")),
		CodeVerifierEncrypted: encrypted, ProviderID: provider.ID,
		ProviderConfigurationVersion: 99, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.test/api/v1/auth/oidc/callback", ReturnPath: "/admin",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	providerServer.setAuthCode("code-version")

	_, _, err = service.HandleCallback(context.Background(), state, "code-version", binding, "127.0.0.1", "agent")
	if !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("version mismatch error = %v", err)
	}
}

func TestListProvidersAndDeleteSuccess(t *testing.T) {
	service, oidc, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)

	listed, err := service.ListProviders(context.Background())
	if err != nil || len(listed) != 1 {
		t.Fatalf("list providers = %v / %v", listed, err)
	}

	if err := service.DeleteProvider(context.Background(), provider.ID, "127.0.0.1", "agent"); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	left, _ := service.ListProviders(context.Background())
	if len(left) != 0 {
		t.Fatalf("providers after delete = %v", left)
	}
	_ = oidc
}

func TestCallbackRedirectPathValidation(t *testing.T) {
	for _, path := range []string{"", "https://evil.test", "/admin/../x", "/api"} {
		if err := CallbackRedirectPath(path); err == nil {
			t.Fatalf("CallbackRedirectPath(%q) accepted", path)
		}
	}
	for _, path := range []string{"/admin", "/admin/security"} {
		if err := CallbackRedirectPath(path); err != nil {
			t.Fatalf("CallbackRedirectPath(%q) rejected: %v", path, err)
		}
	}
}

func TestProviderSecretEmptyReturnsEmpty(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	provider.ClientSecretEncrypted = nil
	secret, err := service.providerSecret(provider)
	if err != nil || secret != "" {
		t.Fatalf("empty provider secret = %q / %v", secret, err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("random source failed") }

func TestBeginLoginRandomFailure(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	service.random = failingReader{}
	if _, err := service.BeginLogin(context.Background(), provider.ID, "/admin",
		"https://beat.test", "127.0.0.1", "agent"); err == nil {
		t.Fatal("begin login with failing random must fail")
	}
}

func TestCompleteLinkRejectsDuplicateSubject(t *testing.T) {
	service, oidc, sqlite, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()

	other := &model.AdminUser{
		ID: "u-other", Username: "other", DisplayName: "Other", Role: model.AdminRoleAdmin,
		PasswordHash: "hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.NewAdminStore(sqlite.DB).CreateUser(context.Background(), other); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	// 同一 subject 已被另一用户链接
	if err := oidc.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-other", ProviderID: provider.ID, UserID: "u-other", Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	state := "link-dup"
	binding := "link-dup"
	encrypted, err := sealOIDCTestResource(service.secrets, []byte("verifier"), secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: "txn-dup",
		Field: "pkce_verifier", Revision: provider.ID, PayloadVersion: "v1",
	})
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	if err := oidc.CreateTransaction(context.Background(), &model.OIDCTransaction{
		ID: "txn-dup", StateHash: sha256sum([]byte(state)),
		BrowserBindingHash: sha256sum([]byte(binding)), NonceHash: sha256sum([]byte("nonce-test")),
		CodeVerifierEncrypted: encrypted, ProviderID: provider.ID,
		ProviderConfigurationVersion: provider.ConfigurationVersion, Purpose: model.OIDCPurposeLink,
		UserID: user.ID, RedirectURI: "https://beat.test/api/v1/auth/oidc/callback",
		ReturnPath: "/admin/security", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	providerServer.setAuthCode("link-dup-code")

	_, _, err = service.HandleCallback(context.Background(), state, "link-dup-code", binding, "127.0.0.1", "agent")
	if !errors.Is(err, ErrIdentityAlreadyLinked) {
		t.Fatalf("duplicate subject error = %v", err)
	}
}

func TestLoginCallbackDisabledUserFails(t *testing.T) {
	service, oidc, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	disabledUser := &model.AdminUser{
		ID: "u-disabled", Username: "disabled-user", DisplayName: "Disabled",
		Role: model.AdminRoleAdmin, PasswordHash: "hash", Enabled: true,
		PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.users.CreateUser(t.Context(), disabledUser); err != nil {
		t.Fatalf("create disabled user: %v", err)
	}
	if err := oidc.LinkIdentity(t.Context(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: "u-disabled", Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	if err := service.users.UpdateUser(t.Context(), "u-disabled", "User", model.AdminRoleAdmin, false); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	state, binding := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("code-disabled")
	_, _, err := service.HandleCallback(t.Context(), state, "code-disabled", binding, "127.0.0.1", "agent")
	if !errors.Is(err, ErrIdentityNotLinked) {
		t.Fatalf("disabled user login error = %v", err)
	}
}

func TestNewServiceUsesDefaults(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	// 显式 nil now/random 应回退默认值
	svc := NewService(service.store, service.users, service.secrets, service.auth, service.client, nil, nil)
	if _, err := svc.ListPublicProviders(t.Context()); err != nil {
		t.Fatalf("defaults service list providers: %v", err)
	}
	if _, err := svc.BeginLogin(t.Context(), provider.ID, "/admin", "https://beat.test", "127.0.0.1", "agent"); err != nil {
		t.Fatalf("defaults service begin: %v", err)
	}
}

func TestVerifyIDTokenRejectsEmptySubject(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	info, err := service.client.Discover(t.Context(), provider.IssuerURL)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	transaction := &model.OIDCTransaction{ID: "txn", NonceHash: sha256sum([]byte("nonce-test"))}
	// 直接调用 verifyIDToken：使用空 subject 签发的令牌
	raw, err := providerServer.signIDToken(provider.IssuerURL, provider.ClientID, "", "nonce-test", nil)
	if err != nil {
		t.Fatalf("sign empty-subject token: %v", err)
	}
	if _, err := service.verifyIDToken(t.Context(), info, provider, transaction, raw); !errors.Is(err, ErrSubjectInvalid) {
		t.Fatalf("empty subject error = %v", err)
	}
}

func TestServiceErrorPathsWithClosedDatabase(t *testing.T) {
	service, _, sqlite, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := context.Background()
	if _, err := service.ListPublicProviders(ctx); err == nil {
		t.Fatal("list public providers on closed db must fail")
	}
	if _, err := service.BeginLogin(ctx, provider.ID, "/admin", "https://beat.test", "127.0.0.1", "agent"); err == nil {
		t.Fatal("begin login on closed db must fail")
	}
	if _, err := service.ListProviders(ctx); err == nil {
		t.Fatal("list providers on closed db must fail")
	}
	if _, err := service.ListIdentitiesForUser(ctx, "u-1"); err == nil {
		t.Fatal("list identities on closed db must fail")
	}
	if err := service.DeleteProvider(ctx, provider.ID, "127.0.0.1", "agent"); err == nil {
		t.Fatal("delete provider on closed db must fail")
	}
	if _, _, err := service.HandleCallback(ctx, "state", "code", "binding", "127.0.0.1", "agent"); err == nil {
		t.Fatal("callback on closed db must fail")
	}
}

type failAfterReader struct {
	remaining int
}

func (r *failAfterReader) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("random source exhausted")
	}
	if len(buffer) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	r.remaining -= len(buffer)
	return len(buffer), nil
}

func TestBeginLoginRandomFailureAtEachStep(t *testing.T) {
	service, _, _, _, providerServer, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	// state=32, nonce=24, verifier=48, binding=32 字节；在每一步耗尽其上限前失败
	for _, failAfter := range []int{0, 32, 32 + 24, 32 + 24 + 48} {
		service.random = &failAfterReader{remaining: failAfter}
		if _, err := service.BeginLogin(t.Context(), provider.ID, "/admin",
			"https://beat.test", "127.0.0.1", "agent"); err == nil {
			t.Fatalf("begin login with random failing after %d bytes must fail", failAfter)
		}
	}
}

func TestLoginCallbackTokenExchangeFailure(t *testing.T) {
	service, oidc, _, _, providerServer, user := setupOIDCService(t)
	provider := createTestProvider(t, service, providerServer.baseURL(), true)
	now := time.Now().UTC()
	_ = oidc.LinkIdentity(t.Context(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: user.ID, Subject: "sub-test-user",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	})

	state, binding := createLoginTransaction(t, service.secrets, oidc, provider)
	providerServer.setAuthCode("expected-code")
	// 使用错误授权码触发令牌端点拒绝
	if _, _, err := service.HandleCallback(t.Context(), state, "wrong-code", binding, "127.0.0.1", "agent"); err == nil {
		t.Fatal("token exchange with wrong code must fail")
	}
}
