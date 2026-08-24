package oidc

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateIssuer(t *testing.T) {
	policy := EndpointPolicy{AllowPrivate: true}
	valid := []string{
		"https://accounts.example.com",
		"https://accounts.example.com/",
	}
	for _, raw := range valid {
		if err := policy.ValidateIssuer(raw); err != nil {
			t.Fatalf("valid issuer %q rejected: %v", raw, err)
		}
	}
	invalid := []string{
		"http://accounts.example.com",      // 非 https
		"https://user:pass@example.com",    // 用户信息
		"https://example.com/path?query=1", // 查询串
		"https://example.com/path#frag",    // 片段
		"not-a-url",
	}
	for _, raw := range invalid {
		if err := policy.ValidateIssuer(raw); err == nil {
			t.Fatalf("invalid issuer %q accepted", raw)
		}
	}
}

func TestValidateIssuerRejectsPrivateAddress(t *testing.T) {
	policy := EndpointPolicy{} // 生产策略：禁止私有地址
	if err := policy.ValidateIssuer("https://127.0.0.1:8080/issuer"); err == nil {
		t.Fatal("loopback issuer must be rejected in production policy")
	}
	if err := policy.ValidateIssuer("https://10.0.0.1/issuer"); err == nil {
		t.Fatal("private issuer must be rejected in production policy")
	}
	if err := policy.ValidateIssuer("https://[::1]/issuer"); err == nil {
		t.Fatal("link-local issuer must be rejected in production policy")
	}
}

func TestAllowPrivateForTest(t *testing.T) {
	policy := AllowPrivateForTest()
	if !policy.AllowPrivate {
		t.Fatal("test policy must allow private addresses")
	}
	if err := policy.ValidateIssuer("https://127.0.0.1:8080/issuer"); err != nil {
		t.Fatalf("loopback issuer rejected under test policy: %v", err)
	}
}

func TestProviderClientDiscovery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": "https://%s/issuer",
			"authorization_endpoint": "https://%s/auth",
			"token_endpoint": "https://%s/token",
			"jwks_uri": "https://%s/keys",
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"]
		}`, r.Host, r.Host, r.Host, r.Host)
	}))
	defer server.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
	issuer := server.URL + "/issuer"

	info, err := client.Discover(context.Background(), issuer)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if info.Issuer != issuer {
		t.Fatalf("issuer = %q, want %q", info.Issuer, issuer)
	}
	if !strings.HasSuffix(info.AuthURL, "/auth") {
		t.Fatalf("auth url = %q", info.AuthURL)
	}

	// 缓存命中
	cached, err := client.Discover(context.Background(), issuer)
	if err != nil || cached != info {
		t.Fatalf("cached discovery mismatch: %v", err)
	}
}

func TestProviderClientDiscoveryRejectsMismatchedIssuer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "https://other.test",
			"authorization_endpoint": "https://other.test/auth",
			"token_endpoint": "https://other.test/token"
		}`))
	}))
	defer server.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
	issuer := server.URL + "/issuer"

	if _, err := client.Discover(context.Background(), issuer); err == nil {
		t.Fatal("mismatched issuer must be rejected")
	}
}

func TestProviderClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"big": "` + strings.Repeat("a", maxProviderResponseBytes) + `"}`))
	}))
	defer server.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
	issuer := server.URL + "/issuer"

	if _, err := client.Discover(context.Background(), issuer); err == nil {
		t.Fatal("oversized discovery response must be rejected")
	}
}

func TestAuthorizationURLIncludesPKCE(t *testing.T) {
	info := &ProviderInfo{AuthURL: "https://issuer.test/auth"}
	raw := NewProviderClient(AllowPrivateForTest(), 0, nil).AuthorizationURL(
		info, "client-1", "https://beat.test/cb", "openid profile",
		"state-1", "nonce-1", "challenge-1")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"response_type":         "code",
		"client_id":             "client-1",
		"redirect_uri":          "https://beat.test/cb",
		"scope":                 "openid profile",
		"state":                 "state-1",
		"nonce":                 "nonce-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
	} {
		if query.Get(key) != want {
			t.Fatalf("query %s = %q, want %q", key, query.Get(key), want)
		}
	}
}

func TestNormalizeIssuer(t *testing.T) {
	if got := normalizeIssuer("https://issuer.test/"); got != "https://issuer.test" {
		t.Fatalf("normalize = %q", got)
	}
}

func TestValidateRequestURL(t *testing.T) {
	permissive := AllowPrivateForTest()
	for _, raw := range []string{
		"http://issuer.test/auth", "https://user:pass@issuer.test/auth",
		"https://issuer.test/auth#frag", "ftp://issuer.test/auth",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := permissive.ValidateRequestURL(parsed); err == nil {
			t.Fatalf("ValidateRequestURL(%q) accepted", raw)
		}
	}
	parsed, _ := url.Parse("https://issuer.test/auth")
	if err := permissive.ValidateRequestURL(parsed); err != nil {
		t.Fatalf("https request url rejected: %v", err)
	}
	if parsed == nil {
		t.Fatal("unreachable")
	}
}

func TestValidateRequestURLRejectsLoopbackInProduction(t *testing.T) {
	production := EndpointPolicy{}
	parsed, err := url.Parse("https://127.0.0.1:8080/auth")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := production.ValidateRequestURL(parsed); err == nil {
		t.Fatal("loopback request url accepted in production policy")
	}
}

func TestHTTPClientRedirectBound(t *testing.T) {
	// 重定向链超过 3 跳必须失败
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hop") == "" {
			http.Redirect(w, r, "?hop=1", http.StatusFound)
			return
		}
		hop := r.URL.Query().Get("hop")
		if hop == "1" {
			http.Redirect(w, r, "?hop=2", http.StatusFound)
			return
		}
		if hop == "2" {
			http.Redirect(w, r, "?hop=3", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
	response, err := client.client.Get(server.URL + "/start")
	if err == nil {
		defer func() { _ = response.Body.Close() }()
		t.Fatal("redirect chain beyond limit must fail")
	}
}

func TestDialContextRejectsPrivateIP(t *testing.T) {
	policy := EndpointPolicy{} // 生产策略
	connection, err := policy.dialContext(&net.Dialer{})(context.Background(), "tcp", "127.0.0.1:8080")
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("dial to loopback must be rejected in production policy")
	}
	if !strings.Contains(err.Error(), "not a public") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateIPClassifies(t *testing.T) {
	policy := EndpointPolicy{}
	rejected := []string{"10.0.0.1", "127.0.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "fe80::1"}
	accepted := []string{"8.8.8.8", "1.1.1.1"}
	for _, raw := range rejected {
		ip := net.ParseIP(raw)
		if err := policy.validateIP(ip); err == nil {
			t.Fatalf("ip %s accepted in production", raw)
		}
	}
	for _, raw := range accepted {
		ip := net.ParseIP(raw)
		if err := policy.validateIP(ip); err != nil {
			t.Fatalf("ip %s rejected: %v", raw, err)
		}
	}
	// 测试策略放行全部
	testPolicy := AllowPrivateForTest()
	if err := testPolicy.validateIP(net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("loopback rejected under test policy: %v", err)
	}
}

func TestValidatePublicHostPrivateLiteral(t *testing.T) {
	if err := validatePublicHost("127.0.0.1"); err == nil {
		t.Fatal("loopback literal accepted")
	}
	if err := validatePublicHost("10.0.0.1"); err == nil {
		t.Fatal("private literal accepted")
	}
	if err := validatePublicHost("8.8.8.8"); err != nil {
		t.Fatalf("public literal rejected: %v", err)
	}
	if err := validatePublicHost(""); err == nil {
		t.Fatal("empty host accepted")
	}
}

func TestBoundedBodyRejectsOversizedRead(t *testing.T) {
	body := &boundedBody{reader: io.NopCloser(strings.NewReader(strings.Repeat("x", 10))), remaining: 5}
	buffer := make([]byte, 8)
	read, err := body.Read(buffer)
	if err != nil || read != 5 {
		t.Fatalf("read = %d, err = %v", read, err)
	}
	_, err = body.Read(buffer)
	if err == nil {
		t.Fatal("oversized read must fail")
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestValidatePublicHostDNSFailure(t *testing.T) {
	if err := validatePublicHost("totally-nonexistent-host-xyz.invalid"); err == nil {
		t.Fatal("non-resolvable host must fail")
	}
}

func TestValidateIssuerDNSFailure(t *testing.T) {
	production := EndpointPolicy{}
	if err := production.ValidateIssuer("https://totally-nonexistent-host-xyz.invalid/issuer"); err == nil {
		t.Fatal("non-resolvable issuer must fail in production")
	}
}
