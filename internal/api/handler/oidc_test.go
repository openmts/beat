package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

// handlerTestIdP 提供发现、JWKS 与令牌端点，供 handler 层 OIDC 测试使用。
type handlerTestIdP struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	mu       sync.Mutex
	authCode string
}

func newHandlerTestIdP(t *testing.T) *handlerTestIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &handlerTestIdP{key: key}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": server.URL, "authorization_endpoint": server.URL + "/auth",
				"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
				{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
			}})
		case "/token":
			idp.handleToken(w, r, server.URL)
		default:
			http.NotFound(w, r)
		}
	}))
	idp.server = server
	t.Cleanup(server.Close)
	return idp
}

func (p *handlerTestIdP) handleToken(w http.ResponseWriter, r *http.Request, issuer string) {
	_ = r.ParseForm()
	p.mu.Lock()
	expected := p.authCode
	p.mu.Unlock()
	if r.FormValue("code") == "" || r.FormValue("code") != expected {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raw, err := jwt.Signed(signer).
		Claims(jwt.Claims{
			Issuer: issuer, Subject: "sub-test-user", Audience: jwt.Audience{"client-1"},
			Expiry: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}).
		Claims(map[string]any{"nonce": "nonce-test", "email": "user@example.com"}).
		Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
}

func (p *handlerTestIdP) setAuthCode(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authCode = code
}

func setupOIDCHandler(t *testing.T) (*OIDCHandler, *handlerTestIdP, *model.AdminUser, *store.OIDCStore, secretbox.ResourceService) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	secrets := testutil.NewSecrets(t, sqlite.DB)
	oidcStore := store.NewOIDCStore(sqlite.DB)
	adminStore := store.NewAdminStore(sqlite.DB)

	now := time.Now().UTC()
	owner := &model.AdminUser{
		ID: "owner-1", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner,
		PasswordHash: "hash", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := adminStore.CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}

	auth, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: adminStore, Secrets: secrets, LegacyToken: "bootstrap",
	})
	if err != nil {
		t.Fatalf("create adminauth: %v", err)
	}
	idp := newHandlerTestIdP(t)
	client := oidc.NewProviderClient(oidc.AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(idp.server.Client().Transport.(*http.Transport).TLSClientConfig)
	service := oidc.NewService(oidcStore, adminStore, secrets, auth, client, time.Now, nil)
	return NewOIDCHandler(service), idp, owner, oidcStore, secrets
}

func createHandlerProvider(t *testing.T, handler *OIDCHandler, idp *handlerTestIdP) *model.OIDCProvider {
	t.Helper()
	provider, err := handler.service.CreateProvider(context.Background(), oidc.ProviderInput{
		Slug: "p1", DisplayName: "IdP One", IssuerURL: idp.server.URL,
		ClientID: "client-1", ClientSecret: "s1", Scopes: []string{"openid"}, Enabled: true,
	}, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

func TestOIDCHandlerListPublicProviders(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/providers", nil)
	response := httptest.NewRecorder()
	handler.HandleListPublicProviders(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	providers := envelope.Data
	if len(providers) != 1 || providers[0]["display_name"] != "IdP One" {
		t.Fatalf("providers = %#v", providers)
	}
	if _, leaked := providers[0]["client_id"]; leaked {
		t.Fatal("client_id leaked in public response")
	}
}

func TestOIDCHandlerBeginLoginSetsBindingCookie(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/auth/oidc/providers/"+provider.ID+"/begin",
		strings.NewReader(`{"return_path":"/admin"}`))
	request.SetPathValue("id", provider.ID)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleBeginLogin(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var binding *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == oidcBrowserBindingCookie {
			binding = cookie
		}
	}
	if binding == nil || binding.Value == "" {
		t.Fatal("missing browser binding cookie")
	}
	if binding.HttpOnly != true || binding.SameSite != http.SameSiteLaxMode {
		t.Fatalf("binding cookie attributes = %#v", binding)
	}
	if binding.Path != "/api/v1/auth/oidc/callback" {
		t.Fatalf("binding cookie path = %q", binding.Path)
	}
	var body struct {
		Data struct {
			AuthorizationURL string `json:"authorization_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !strings.Contains(body.Data.AuthorizationURL, "state=") {
		t.Fatalf("body = %s / %v", response.Body.String(), err)
	}
}

func TestOIDCHandlerCallbackGenericFailure(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=x&code=y", nil)
	response := httptest.NewRecorder()
	handler.HandleCallback(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d", response.Code)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin?oidc=") {
		t.Fatalf("location = %q", location)
	}
}

func TestOIDCHandlerUnlinkRequiresPrincipal(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc/identities/id-1", nil)
	response := httptest.NewRecorder()
	handler.HandleUnlink(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestOIDCHandlerCreateProviderBadInput(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc/providers",
		strings.NewReader(`{"slug":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleCreateProvider(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestOIDCHandlerUpdateProviderNotFound(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc/providers/missing",
		strings.NewReader(`{"display_name":"x","client_id":"c","scopes":["openid"]}`))
	request.SetPathValue("id", "missing")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleUpdateProvider(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestOIDCHandlerDeleteProviderGenericFailure(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc/providers/missing", nil)
	request.SetPathValue("id", "missing")
	response := httptest.NewRecorder()
	handler.HandleDeleteProvider(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want generic 400", response.Code)
	}
}

func TestOIDCErrorCategoryMapping(t *testing.T) {
	cases := map[error]string{
		oidc.ErrIdentityNotLinked:  "identity_not_linked",
		oidc.ErrProviderDisabled:   "provider_disabled",
		oidc.ErrTransactionExpired: "transaction_expired",
		oidc.ErrCallbackFailed:     "generic",
	}
	for err, want := range cases {
		if got := oidcErrorCategory(err); got != want {
			t.Fatalf("category(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestOIDCHandlerListIdentitiesMe(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	now := time.Now().UTC()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc/identities/me", nil)
	ctx := middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{
		User: model.AdminUser{ID: "owner-1"}, Session: model.AdminSession{ID: "s-1"},
	})
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	handler.HandleListIdentitiesMe(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	_ = now
}

func TestOIDCHandlerListIdentitiesMeUnauthorized(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc/identities/me", nil)
	response := httptest.NewRecorder()
	handler.HandleListIdentitiesMe(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestOIDCHandlerBeginLinkRequiresRecentAuth(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/oidc/providers/"+provider.ID+"/link",
		strings.NewReader(`{"return_path":"/admin/security"}`))
	request.SetPathValue("id", provider.ID)
	request.Header.Set("Content-Type", "application/json")
	ctx := middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{
		User: model.AdminUser{ID: "owner-1"}, Session: model.AdminSession{ID: "s-1"},
	})
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	handler.HandleBeginLink(response, request)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 412", response.Code)
	}
}

func TestOIDCHandlerListProviders(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc/providers", nil)
	response := httptest.NewRecorder()
	handler.HandleListProviders(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("providers = %d", len(envelope.Data))
	}
	if _, leaked := envelope.Data[0]["client_secret_encrypted"]; leaked {
		t.Fatal("ciphertext leaked in provider response")
	}
}

func TestOIDCHandlerDeleteProviderSuccess(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc/providers/"+provider.ID, nil)
	request.SetPathValue("id", provider.ID)
	response := httptest.NewRecorder()
	handler.HandleDeleteProvider(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOIDCHandlerCreateProviderDiscoveryFailure(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc/providers",
		strings.NewReader(`{"slug":"bad","display_name":"Bad","issuer_url":"https://unreachable.invalid",`+
			`"client_id":"c","client_secret":"s","scopes":["openid"],"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleCreateProvider(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestOIDCHandlerUpdateProviderSuccess(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc/providers/"+provider.ID,
		strings.NewReader(`{"display_name":"Renamed","client_id":"c1","scopes":["openid"],"enabled":false}`))
	request.SetPathValue("id", provider.ID)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleUpdateProvider(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatal("provider should be disabled after update")
	}
}

func TestOIDCHandlerCallbackLoginSuccessAtHandlerLevel(t *testing.T) {
	handler, idp, owner, oidcStore, secrets := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := oidcStore.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: owner.ID, Subject: "sub-test-user",
		Email: "user@example.com", LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	state := "state-handler"
	binding := "binding-handler"
	stateHash := sha256.Sum256([]byte(state))
	bindingHash := sha256.Sum256([]byte(binding))
	nonceHash := sha256.Sum256([]byte("nonce-test"))
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("verifier"), AAD: oidc.TransactionVerifierAAD("txn-handler", provider.ID), Persist: func(ciphertext []byte) error {
		return oidcStore.CreateTransaction(ctx, &model.OIDCTransaction{
			ID: "txn-handler", StateHash: stateHash[:], BrowserBindingHash: bindingHash[:],
			NonceHash: nonceHash[:], CodeVerifierEncrypted: ciphertext,
			ProviderID: provider.ID, ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
			RedirectURI: "http://example.com/api/v1/auth/oidc/callback", ReturnPath: "/admin",
			CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		})
	}})
	idp.setAuthCode("code-handler")

	request := httptest.NewRequest(http.MethodGet,
		"http://example.com/api/v1/auth/oidc/callback?state="+state+"&code=code-handler", nil)
	request.AddCookie(&http.Cookie{Name: oidcBrowserBindingCookie, Value: binding})
	response := httptest.NewRecorder()
	handler.HandleCallback(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body = %s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/admin" {
		t.Fatalf("location = %q", location)
	}
	var sessionSet, bindingExpired bool
	for _, cookie := range response.Result().Cookies() {
		switch cookie.Name {
		case "beat_admin_session":
			if cookie.Value != "" {
				sessionSet = true
			}
		case oidcBrowserBindingCookie:
			if cookie.MaxAge < 0 {
				bindingExpired = true
			}
		}
	}
	if !sessionSet {
		t.Fatal("callback login did not set session cookie")
	}
	if !bindingExpired {
		t.Fatal("callback login did not expire binding cookie")
	}
}

func TestOIDCHandlerUnlinkSuccess(t *testing.T) {
	handler, idp, owner, oidcStore, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := oidcStore.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: owner.ID, Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	recent := now.Add(time.Minute)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc/identities/id-1", nil)
	request.SetPathValue("id", "id-1")
	principal := model.AdminPrincipal{
		User:    *owner,
		Session: model.AdminSession{ReauthenticatedUntil: &recent},
	}
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	handler.HandleUnlink(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	left, err := handler.service.ListIdentitiesForUser(ctx, owner.ID)
	if err != nil || len(left) != 0 {
		t.Fatalf("identities after unlink = %v / %v", left, err)
	}
}

func TestOIDCHandlerBeginLoginErrorPaths(t *testing.T) {
	handler, idp, _, _, _ := setupOIDCHandler(t)
	disabled := createDisabledProvider(t, handler, idp)

	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/auth/oidc/providers/"+disabled.ID+"/begin",
		strings.NewReader(`{"return_path":"/admin"}`))
	request.SetPathValue("id", disabled.ID)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleBeginLogin(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("disabled provider status = %d, want 409", response.Code)
	}

	badBody := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/providers/x/begin",
		strings.NewReader(`{"return_path":}`))
	badBody.Header.Set("Content-Type", "application/json")
	badResponse := httptest.NewRecorder()
	handler.HandleBeginLogin(badResponse, badBody)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d, want 400", badResponse.Code)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/providers/missing/begin",
		strings.NewReader(`{"return_path":"/admin"}`))
	missing.SetPathValue("id", "missing")
	missing.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	handler.HandleBeginLogin(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d, want 404", missingResponse.Code)
	}
}

func createDisabledProvider(t *testing.T, handler *OIDCHandler, idp *handlerTestIdP) *model.OIDCProvider {
	t.Helper()
	provider, err := handler.service.CreateProvider(context.Background(), oidc.ProviderInput{
		Slug: "p-disabled", DisplayName: "Disabled", IssuerURL: idp.server.URL,
		ClientID: "c", ClientSecret: "s", Scopes: []string{"openid"}, Enabled: false,
	}, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("create disabled provider: %v", err)
	}
	return provider
}

func TestOIDCHandlerBeginLinkSuccess(t *testing.T) {
	handler, idp, owner, _, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)

	recent := time.Now().UTC().Add(time.Minute)
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/oidc/providers/"+provider.ID+"/link",
		strings.NewReader(`{"return_path":"/admin/security"}`))
	request.SetPathValue("id", provider.ID)
	request.Header.Set("Content-Type", "application/json")
	principal := model.AdminPrincipal{
		User:    *owner,
		Session: model.AdminSession{ReauthenticatedUntil: &recent},
	}
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	handler.HandleBeginLink(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOIDCHandlerDeleteProviderWithIdentitiesConflict(t *testing.T) {
	handler, idp, owner, oidcStore, _ := setupOIDCHandler(t)
	provider := createHandlerProvider(t, handler, idp)
	now := time.Now().UTC()
	if err := oidcStore.LinkIdentity(context.Background(), &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: owner.ID, Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc/providers/"+provider.ID, nil)
	request.SetPathValue("id", provider.ID)
	response := httptest.NewRecorder()
	handler.HandleDeleteProvider(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
}

func TestOIDCHandlerBeginLoginUnavailableProvider(t *testing.T) {
	handler, _, _, _, _ := setupOIDCHandler(t)
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/auth/oidc/providers/missing/begin",
		strings.NewReader(`{"return_path":"/admin"}`))
	request.SetPathValue("id", "missing")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleBeginLogin(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestWriteOIDCErrorStatusMapping(t *testing.T) {
	cases := map[error]int{
		oidc.ErrProviderNotFound:      http.StatusNotFound,
		oidc.ErrProviderDisabled:      http.StatusConflict,
		oidc.ErrProviderInUse:         http.StatusConflict,
		oidc.ErrIssuerImmutable:       http.StatusBadRequest,
		oidc.ErrInvalidProviderInput:  http.StatusBadRequest,
		oidc.ErrRecentAuthRequired:    http.StatusPreconditionRequired,
		oidc.ErrIdentityNotLinked:     http.StatusConflict,
		oidc.ErrIdentityAlreadyLinked: http.StatusConflict,
		oidc.ErrIdentityConflict:      http.StatusConflict,
		oidc.ErrIdentityNotFound:      http.StatusNotFound,
		oidc.ErrCallbackFailed:        http.StatusBadRequest,
		oidc.ErrTransactionExpired:    http.StatusBadRequest,
		errors.New("unexpected"):      http.StatusBadRequest,
	}
	for err, want := range cases {
		response := httptest.NewRecorder()
		writeOIDCError(response, err)
		if response.Code != want {
			t.Fatalf("writeOIDCError(%v) = %d, want %d", err, response.Code, want)
		}
	}
}

func TestOIDCHandlerStoreFailurePaths(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	oidcStore := store.NewOIDCStore(sqlite.DB)
	adminStore := store.NewAdminStore(sqlite.DB)
	secrets := testutil.NewSecrets(t, sqlite.DB)
	auth, err := adminauth.NewService(adminauth.ServiceConfig{Store: adminStore, Secrets: secrets})
	if err != nil {
		t.Fatalf("new adminauth: %v", err)
	}
	client := oidc.NewProviderClient(oidc.AllowPrivateForTest(), time.Minute, time.Now)
	service := oidc.NewService(oidcStore, adminStore, secrets, auth, client, time.Now, nil)
	handler := NewOIDCHandler(service)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/providers", nil)
	response := httptest.NewRecorder()
	handler.HandleListPublicProviders(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("public providers status = %d, want 500", response.Code)
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc/providers", nil)
	adminResponse := httptest.NewRecorder()
	handler.HandleListProviders(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusInternalServerError {
		t.Fatalf("admin providers status = %d, want 500", adminResponse.Code)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc/identities/me", nil)
	me = me.WithContext(middleware.WithAdminPrincipal(me.Context(), model.AdminPrincipal{
		User: model.AdminUser{ID: "u-1"}, Session: model.AdminSession{ID: "s-1"},
	}))
	meResponse := httptest.NewRecorder()
	handler.HandleListIdentitiesMe(meResponse, me)
	if meResponse.Code != http.StatusInternalServerError {
		t.Fatalf("identities/me status = %d, want 500", meResponse.Code)
	}
}
