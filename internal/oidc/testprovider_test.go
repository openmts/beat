package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// testOIDCProvider 是可签发真实 RS256 ID 令牌的 loopback TLS OIDC 提供方。
type testOIDCProvider struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu       sync.Mutex
	nonces   map[string]bool // 已消费的 nonce
	authCode string          // 期待的授权码
}

func newTestOIDCProvider(t interface{ Cleanup(func()) }) *testOIDCProvider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("generate test rsa key: " + err.Error())
	}
	provider := &testOIDCProvider{key: key, nonces: make(map[string]bool)}
	server := httptest.NewTLSServer(provider.handler())
	provider.server = server
	t.Cleanup(server.Close)
	return provider
}

// baseURL 返回 loopback TLS 服务器地址（含 scheme）。
func (p *testOIDCProvider) baseURL() string { return p.server.URL }

// signIDToken 签发受支持的 ID 令牌。
func (p *testOIDCProvider) signIDToken(
	issuer, clientID, subject, nonce string, claims map[string]any,
) (string, error) {
	builder := jwt.Signed(p.signer()).
		Claims(jwt.Claims{
			Issuer:   issuer,
			Subject:  subject,
			Audience: jwt.Audience{clientID},
			Expiry:   jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		}).
		Claims(map[string]any{"nonce": nonce})
	if len(claims) > 0 {
		builder = builder.Claims(claims)
	}
	signed, err := builder.Serialize()
	if err != nil {
		return "", err
	}
	return signed, nil
}

func (p *testOIDCProvider) signer() jose.Signer {
	signingKey := jose.SigningKey{Algorithm: jose.RS256, Key: p.key}
	signer, err := jose.NewSigner(signingKey, &jose.SignerOptions{})
	if err != nil {
		panic("create signer: " + err.Error())
	}
	return signer
}

func (p *testOIDCProvider) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := p.server.URL
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                issuer,
				"authorization_endpoint":                issuer + "/auth",
				"token_endpoint":                        issuer + "/token",
				"jwks_uri":                              issuer + "/keys",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
				{Key: &p.key.PublicKey, KeyID: "test-key-1", Algorithm: "RS256", Use: "sig"},
			}}
			_ = json.NewEncoder(w).Encode(jwks)
		case "/token":
			p.handleToken(w, r, issuer)
		default:
			http.NotFound(w, r)
		}
	})
}

// handleToken 校验 code_verifier 并返回 ID 令牌。
func (p *testOIDCProvider) handleToken(w http.ResponseWriter, r *http.Request, issuer string) {
	_ = r.ParseForm()
	code := r.FormValue("code")
	verifier := r.FormValue("code_verifier")
	p.mu.Lock()
	expectedCode := p.authCode
	p.mu.Unlock()
	if code == "" || code != expectedCode {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	_ = verifier // 测试提供方不严格校验 PKCE 验证器
	subject := "sub-test-user"
	raw, err := p.signIDToken(issuer, "client-1", subject, "nonce-test",
		map[string]any{"email": "user@example.com", "email_verified": true, "name": "Test User"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
}

// setAuthCode 设置下一个可兑换的授权码。
func (p *testOIDCProvider) setAuthCode(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authCode = code
}
