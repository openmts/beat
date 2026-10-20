package api

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

// routerTestIdP 提供发现、JWKS 与令牌端点，供路由层 OIDC 测试使用。
type routerTestIdP struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	mu       sync.Mutex
	authCode string
}

func newRouterTestIdP(t *testing.T) *routerTestIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &routerTestIdP{key: key}
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

func (p *routerTestIdP) handleToken(w http.ResponseWriter, r *http.Request, issuer string) {
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

func (p *routerTestIdP) setAuthCode(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authCode = code
}

func newTestSecrets(t *testing.T, database *sql.DB) secretbox.ResourceService {
	t.Helper()
	return testutil.NewSecrets(t, database)
}

func newTestSecurity(t *testing.T, adminStore *store.AdminStore, secrets secretbox.ResourceService) *adminauth.Service {
	t.Helper()
	randomData := make([]byte, 8192)
	for index := range randomData {
		randomData[index] = byte(index)
	}
	random := bytes.NewReader(randomData)
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: adminStore, Secrets: secrets, LegacyToken: "admin-secret", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	return security
}

func setupOIDCRouter(t *testing.T) (*Router, *routerTestIdP, *http.Cookie, *store.SQLiteStore, *store.AdminStore, secretbox.ResourceService) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	adminStore := store.NewAdminStore(sqlite.DB)
	secrets := newTestSecrets(t, sqlite.DB)
	security := newTestSecurity(t, adminStore, secrets)

	idp := newRouterTestIdP(t)
	client := oidc.NewProviderClient(oidc.AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(idp.server.Client().Transport.(*http.Transport).TLSClientConfig)
	oidcService := oidc.NewService(store.NewOIDCStore(sqlite.DB), adminStore, secrets, security, client, time.Now, nil)

	router := NewRouter(store.NewNodeStore(sqlite.DB), store.NewGroupStore(sqlite.DB),
		store.NewSSHKeyStore(sqlite.DB), store.NewAlertRuleStore(sqlite.DB),
		store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		WithAdminToken("admin-secret"), WithAdminSecurity(security), WithOIDC(oidcService))

	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	return router, idp, cookie, sqlite, adminStore, secrets
}

func TestRouterOIDCPublicAndBegin(t *testing.T) {
	router, idp, cookie, _, _, _ := setupOIDCRouter(t)

	// 创建提供方需要最近重认证
	withoutReauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/oidc/providers",
		`{"slug":"p1","display_name":"IdP","issuer_url":"`+idp.server.URL+`",`+
			`"client_id":"c1","client_secret":"s1","scopes":["openid"],"enabled":true}`,
		cookie, "http://example.com")
	if withoutReauth.Code != http.StatusPreconditionRequired {
		t.Fatalf("create without reauth status = %d, body = %s", withoutReauth.Code, withoutReauth.Body.String())
	}

	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate status = %d, body = %s", reauth.Code, reauth.Body.String())
	}

	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/oidc/providers",
		`{"slug":"p1","display_name":"IdP","issuer_url":"`+idp.server.URL+`",`+
			`"client_id":"c1","client_secret":"s1","scopes":["openid"],"enabled":true}`,
		cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider status = %d, body = %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "s1") {
		t.Fatal("client secret leaked in provider response")
	}
	var createdBody struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil || createdBody.Data.ID == "" {
		t.Fatalf("decode provider = %v", err)
	}

	// 公共端点无需登录
	public := requestRouter(router, http.MethodGet, "/api/v1/auth/oidc/providers", "", "", "")
	if public.Code != http.StatusOK || !strings.Contains(public.Body.String(), "IdP") {
		t.Fatalf("public providers status = %d, body = %s", public.Code, public.Body.String())
	}
	if strings.Contains(public.Body.String(), `"client_id"`) {
		t.Fatal("client_id leaked to public endpoint")
	}

	// 开始登录（公开，无管理员会话也可）
	begin := requestRouter(router, http.MethodPost, "/api/v1/auth/oidc/providers/"+createdBody.Data.ID+"/begin",
		`{"return_path":"/admin"}`, "", "http://example.com")
	if begin.Code != http.StatusOK || !strings.Contains(begin.Body.String(), "authorization_url") {
		t.Fatalf("begin login status = %d, body = %s", begin.Code, begin.Body.String())
	}
	var binding *http.Cookie
	for _, cookie := range begin.Result().Cookies() {
		if cookie.Name == "beat_oidc_binding" {
			binding = cookie
		}
	}
	if binding == nil || binding.Value == "" {
		t.Fatal("begin login did not set binding cookie")
	}
}

func TestRouterOIDCOwnerOnlyProviderManagement(t *testing.T) {
	router, _, cookie, _, _, _ := setupOIDCRouter(t)

	reauthenticated := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauthenticated.Code != http.StatusOK {
		t.Fatalf("owner reauthentication status = %d", reauthenticated.Code)
	}

	// 创建普通 admin 并登录
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/users",
		`{"username":"operator","display_name":"Operator","role":"admin",`+
			`"password":"operator password long"}`, cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create admin status = %d", created.Code)
	}
	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login",
		`{"username":"operator","password":"operator password long"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	operatorCookie := login.Result().Cookies()[0]

	list := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/oidc/providers", "", operatorCookie, "http://example.com")
	if list.Code != http.StatusForbidden {
		t.Fatalf("admin provider list status = %d, want 403", list.Code)
	}

	// 身份列表（普通 admin 可用）
	identities := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/oidc/identities/me", "", operatorCookie, "http://example.com")
	if identities.Code != http.StatusOK {
		t.Fatalf("identities/me status = %d", identities.Code)
	}
}

func TestRouterOIDCCallbackGenericWithoutBinding(t *testing.T) {
	router, _, _, _, _, _ := setupOIDCRouter(t)
	request := httptest.NewRequest(http.MethodGet,
		"http://example.com/api/v1/auth/oidc/callback?state=s&code=c", nil)
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want redirect", response.Code)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin?oidc=") {
		t.Fatalf("callback location = %q", location)
	}
}

func TestRouterOIDCCallbackLoginSuccess(t *testing.T) {
	router, idp, _, sqlite, adminStore, secrets := setupOIDCRouter(t)
	ctx := t.Context()
	oidcStore := store.NewOIDCStore(sqlite.DB)

	provider := &model.OIDCProvider{
		ID: "p1", Slug: "issuer", DisplayName: "IdP", IssuerURL: idp.server.URL,
		ClientID: "client-1", Scopes: []string{"openid"},
		Enabled: true, ConfigurationVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("client-secret"), AAD: oidc.ProviderSecretAAD("p1", 1),
		Persist: func(ciphertext []byte) error {
			provider.ClientSecretEncrypted = ciphertext
			return oidcStore.CreateProvider(ctx, provider)
		}})
	owner, err := adminStore.GetUserByUsername(ctx, "owner")
	if err != nil || owner == nil {
		t.Fatalf("get owner: %v / %v", owner, err)
	}
	now := time.Now().UTC()
	if err := oidcStore.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-1", ProviderID: provider.ID, UserID: owner.ID, Subject: "sub-test-user",
		Email: "user@example.com", LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	state := "state-router"
	binding := "binding-router"
	stateHash := sha256.Sum256([]byte(state))
	bindingHash := sha256.Sum256([]byte(binding))
	nonceHash := sha256.Sum256([]byte("nonce-test"))
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("verifier"), AAD: oidc.TransactionVerifierAAD("txn-router", provider.ID), Persist: func(ciphertext []byte) error {
		return oidcStore.CreateTransaction(ctx, &model.OIDCTransaction{
			ID: "txn-router", StateHash: stateHash[:], BrowserBindingHash: bindingHash[:],
			NonceHash: nonceHash[:], CodeVerifierEncrypted: ciphertext, ProviderID: provider.ID,
			ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
			RedirectURI: "http://example.com/api/v1/auth/oidc/callback", ReturnPath: "/admin",
			CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		})
	}})
	idp.setAuthCode("code-router")

	request := httptest.NewRequest(http.MethodGet,
		"http://example.com/api/v1/auth/oidc/callback?state="+state+"&code=code-router", nil)
	request.AddCookie(&http.Cookie{Name: "beat_oidc_binding", Value: binding})
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body = %s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/admin" {
		t.Fatalf("location = %q, want /admin", location)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "beat_admin_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback login did not set admin session cookie")
	}
}
