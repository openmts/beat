package oidc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ProviderInfo 是从发现元数据解析出的提供方信息。
type ProviderInfo struct {
	Issuer   string
	AuthURL  string
	TokenURL string
	Provider *oidc.Provider
}

// ProviderClient 负责安全的提供方发现与 ID 令牌验证，封装 SSRF 策略。
type ProviderClient struct {
	policy EndpointPolicy
	client *http.Client

	mu       sync.Mutex
	cached   map[string]cachedProvider
	cacheTTL time.Duration
	now      func() time.Time
}

type cachedProvider struct {
	info    *ProviderInfo
	expires time.Time
}

// NewProviderClient 构造提供方客户端；cacheTTL 为发现缓存时长。
func NewProviderClient(policy EndpointPolicy, cacheTTL time.Duration, now func() time.Time) *ProviderClient {
	if cacheTTL <= 0 {
		cacheTTL = 15 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &ProviderClient{
		policy: policy, client: policy.newHTTPClient(), cached: make(map[string]cachedProvider),
		cacheTTL: cacheTTL, now: now,
	}
}

// SetTLSClientConfig 注入自定义根证书；仅测试提供的 loopback TLS 服务器使用。
func (client *ProviderClient) SetTLSClientConfig(config *tls.Config) {
	if bounded, ok := client.client.Transport.(*boundedTransport); ok {
		if transport, ok := bounded.inner.(*http.Transport); ok {
			transport.TLSClientConfig = config.Clone()
		}
	}
}

// Discover 获取提供方发现元数据（有界缓存，键轮换后过期重取）。
func (client *ProviderClient) Discover(ctx context.Context, issuer string) (*ProviderInfo, error) {
	issuer = normalizeIssuer(issuer)
	client.mu.Lock()
	if cached, ok := client.cached[issuer]; ok && client.now().Before(cached.expires) {
		client.mu.Unlock()
		return cached.info, nil
	}
	client.mu.Unlock()

	info, err := client.discoverUncached(ctx, issuer)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	client.cached[issuer] = cachedProvider{info: info, expires: client.now().Add(client.cacheTTL)}
	client.mu.Unlock()
	return info, nil
}

func (client *ProviderClient) discoverUncached(ctx context.Context, issuer string) (*ProviderInfo, error) {
	if err := client.policy.ValidateIssuer(issuer); err != nil {
		return nil, err
	}
	oidcCtx := oidc.ClientContext(ctx, client.client)
	provider, err := oidc.NewProvider(oidcCtx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover provider: %w", err)
	}
	return &ProviderInfo{
		Issuer:   normalizeIssuer(issuer),
		AuthURL:  provider.Endpoint().AuthURL,
		TokenURL: provider.Endpoint().TokenURL,
		Provider: provider,
	}, nil
}

// ExchangeCode 使用 PKCE 验证器与固定回调地址交换授权码，返回原始 ID 令牌。
func (client *ProviderClient) ExchangeCode(
	ctx context.Context,
	info *ProviderInfo,
	clientID, clientSecret, redirectURI, code, codeVerifier string,
	scopes []string,
) (string, error) {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: info.AuthURL, TokenURL: info.TokenURL},
		RedirectURL:  redirectURI,
		Scopes:       scopes,
	}
	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, client.client)
	token, err := config.Exchange(exchangeCtx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return "", fmt.Errorf("oidc: exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", errors.New("oidc: token response missing id_token")
	}
	return rawIDToken, nil
}

// Verifier 构造 ID 令牌验证器；签名算法默认取提供方声明集合。
func (client *ProviderClient) Verifier(
	ctx context.Context, info *ProviderInfo, clientID string,
) *oidc.IDTokenVerifier {
	return info.Provider.VerifierContext(oidc.ClientContext(ctx, client.client),
		&oidc.Config{ClientID: clientID})
}

// AuthorizationURL 构建带 state/nonce/PKCE S256 的授权 URL。
func (client *ProviderClient) AuthorizationURL(
	info *ProviderInfo, clientID, redirectURI, scopesJoined, state, nonce, codeChallenge string,
) string {
	values := map[string]string{
		"response_type":         "code",
		"client_id":             clientID,
		"redirect_uri":          redirectURI,
		"scope":                 scopesJoined,
		"state":                 state,
		"nonce":                 nonce,
		"code_challenge":        codeChallenge,
		"code_challenge_method": "S256",
	}
	built := info.AuthURL + "?"
	first := true
	for key, value := range values {
		if !first {
			built += "&"
		}
		first = false
		built += key + "=" + url.QueryEscape(value)
	}
	return built
}

// AllowPrivateForTest 返回允许回环/私有地址的测试策略。
func AllowPrivateForTest() EndpointPolicy {
	return EndpointPolicy{AllowPrivate: true}
}
