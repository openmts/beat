package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

func TestProviderClientDiscoveryRejectsUnsafeEndpoints(t *testing.T) {
	var unsafeField string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		issuer := "https://" + request.Host
		values := map[string]string{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/auth",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/keys",
			"response_types_supported":              "code",
			"subject_types_supported":               "public",
			"id_token_signing_alg_values_supported": "RS256",
		}
		values[unsafeField] = "http://insecure.test/endpoint"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(values)
	}))
	defer server.Close()
	for _, field := range []string{"authorization_endpoint", "token_endpoint", "jwks_uri"} {
		t.Run(field, func(t *testing.T) {
			unsafeField = field
			client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
			client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
			if _, err := client.Discover(t.Context(), server.URL+"/issuer"); err == nil {
				t.Fatalf("unsafe %s accepted", field)
			}
		})
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
	info := &ProviderInfo{AuthURL: "https://issuer.test/auth?tenant=acme&prompt=select_account"}
	raw := NewProviderClient(AllowPrivateForTest(), 0, nil).AuthorizationURL(
		info, "client-1", "https://beat.test/cb", "openid profile",
		"state-1", "nonce-1", "challenge-1")
	if strings.Count(raw, "?") != 1 {
		t.Fatalf("authorization URL has duplicated query separator: %q", raw)
	}
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
		"tenant":                "acme",
		"prompt":                "select_account",
	} {
		if query.Get(key) != want {
			t.Fatalf("query %s = %q, want %q", key, query.Get(key), want)
		}
	}
}

func TestHTTPClientTotalTimeoutCoversResponseBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
	if client.client.Timeout <= 0 {
		t.Fatal("provider HTTP client must have an overall request timeout")
	}
	client.client.Timeout = 250 * time.Millisecond
	response, err := client.client.Get(server.URL)
	if err != nil {
		t.Fatalf("request headers: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if _, err = io.ReadAll(response.Body); err == nil {
		t.Fatal("a stalled response body must fail closed at the overall timeout")
	}
}

func TestExchangeCodeRejectsCrossOriginRedirectBeforeRequest(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stolen","token_type":"Bearer","id_token":"stolen"}`))
	}))
	defer target.Close()

	var sourceRequests atomic.Int32
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/token" {
			t.Errorf("unexpected token request: %s %s", r.Method, r.URL.Path)
		}
		http.Redirect(w, r, target.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := NewProviderClient(AllowPrivateForTest(), time.Minute, time.Now)
	client.SetTLSClientConfig(source.Client().Transport.(*http.Transport).TLSClientConfig)
	info := &ProviderInfo{AuthURL: source.URL + "/auth", TokenURL: source.URL + "/token"}
	_, err := client.ExchangeCode(
		t.Context(), info, "client-1", "client-secret", "https://beat.test/cb", "code-1", "verifier-1", nil,
	)
	if err == nil {
		t.Fatal("cross-origin token redirect must be rejected")
	}
	if sourceRequests.Load() == 0 {
		t.Fatal("source token endpoint did not receive the exchange request")
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d token requests", targetRequests.Load())
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
	rejected := []string{
		"10.0.0.1", "127.0.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0",
		"100.64.0.1", "100.100.100.200", "192.0.2.1", "198.18.0.1", "203.0.113.1",
		"fe80::1", "2001:db8::1", "2001:2::1", "2002::1", "3fff::1", "64:ff9b::a00:1",
	}
	accepted := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
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

func TestDialContextAllowsOnlyConfiguredProxyEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	policy := EndpointPolicy{AllowProxyDial: func(_ context.Context, address string) (bool, error) {
		return address == listener.Addr().String(), nil
	}}
	connection, err := policy.dialContext(&net.Dialer{})(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := listener.Accept()
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	_ = accepted.Close()
	_ = connection.Close()

	policy.AllowProxyDial = func(context.Context, string) (bool, error) { return false, errors.New("fixture") }
	if _, err := policy.dialContext(&net.Dialer{})(t.Context(), "tcp", listener.Addr().String()); err == nil || !strings.Contains(err.Error(), "fixture") {
		t.Fatalf("proxy policy error was not preserved: %v", err)
	}
}

func TestCheckedDialPinsValidatedAddress(t *testing.T) {
	dialed := ""
	policy := EndpointPolicy{}
	left, right := net.Pipe()
	defer func() { _ = left.Close(); _ = right.Close() }()
	dial := policy.checkedDial(oidcDialNetwork{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = address
			return left, nil
		},
	})
	connection, err := dial(t.Context(), "tcp", "issuer.example:443")
	if err != nil || connection != left || dialed != "93.184.216.34:443" {
		t.Fatalf("validated address was not pinned: connection=%v dialed=%q err=%v", connection, dialed, err)
	}
}

func TestCheckedDialRejectsMixedInvalidAndOversizedAnswers(t *testing.T) {
	cases := map[string][]net.IPAddr{
		"private": {{IP: net.ParseIP("10.0.0.1")}},
		"mixed":   {{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}},
		"zoned":   {{IP: net.ParseIP("2606:4700:4700::1111"), Zone: "eth0"}},
		"nil":     {{}},
		"empty":   {},
		"too many": func() []net.IPAddr {
			addresses := make([]net.IPAddr, maximumOIDCAddresses+1)
			for index := range addresses {
				addresses[index].IP = net.ParseIP("93.184.216.34")
			}
			return addresses
		}(),
	}
	for name, addresses := range cases {
		t.Run(name, func(t *testing.T) {
			dialed := false
			dial := EndpointPolicy{}.checkedDial(oidcDialNetwork{
				lookup: func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil },
				dial: func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, errors.New("must not dial")
				},
			})
			if _, err := dial(t.Context(), "tcp", "issuer.example:443"); err == nil || dialed {
				t.Fatalf("unsafe answer accepted: err=%v dialed=%v", err, dialed)
			}
		})
	}
}

func TestCheckedDialFallsBackAcrossValidatedAddresses(t *testing.T) {
	var dialed []string
	left, right := net.Pipe()
	defer func() { _ = left.Close(); _ = right.Close() }()
	dial := EndpointPolicy{}.checkedDial(oidcDialNetwork{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("2606:4700:4700::1111")}}, nil
		},
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if len(dialed) == 1 {
				return nil, errors.New("unreachable")
			}
			return left, nil
		},
	})
	connection, err := dial(t.Context(), "tcp", "issuer.example:443")
	if err != nil || connection != left || strings.Join(dialed, ",") != "93.184.216.34:443,[2606:4700:4700::1111]:443" {
		t.Fatalf("fallback failed: connection=%v dialed=%v err=%v", connection, dialed, err)
	}
}

func TestHTTPClientRejectsPrivateTargetBeforeCallingProxy(t *testing.T) {
	proxyCalled := false
	policy := EndpointPolicy{
		Proxy: func(*http.Request) (*url.URL, error) {
			proxyCalled = true
			return nil, errors.New("proxy fixture")
		},
	}
	response, err := policy.newHTTPClient().Get("https://127.0.0.1:8080/token")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("private proxy target must be rejected")
	}
	if proxyCalled {
		t.Fatal("private target reached the proxy selector")
	}
}

func TestHTTPClientValidatesPublicProxyTarget(t *testing.T) {
	proxyCalled := false
	policy := EndpointPolicy{Proxy: func(*http.Request) (*url.URL, error) {
		proxyCalled = true
		return nil, errors.New("proxy fixture")
	}}
	response, err := policy.newHTTPClient().Get("https://8.8.8.8/token")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "proxy fixture") {
		t.Fatalf("public target did not reach proxy: %v", err)
	}
	if !proxyCalled {
		t.Fatal("public target was rejected before the proxy selector")
	}
}

func TestValidatePublicHostRejectsEmptyAndNilResolution(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }
	if err := validatePublicHostWithLookup(t.Context(), "empty.test", lookup); err == nil {
		t.Fatal("empty DNS result accepted")
	}
	lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{}}, nil
	}
	if err := validatePublicHostWithLookup(t.Context(), "nil.test", lookup); err == nil {
		t.Fatal("nil DNS address accepted")
	}
	lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("100.100.100.200")}}, nil
	}
	if err := validatePublicHostWithLookup(t.Context(), "metadata.test", lookup); err == nil {
		t.Fatal("reserved DNS address accepted")
	}
	lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return make([]net.IPAddr, maximumOIDCAddresses+1), nil
	}
	if err := validatePublicHostWithLookup(t.Context(), "oversized.test", lookup); err == nil {
		t.Fatal("oversized DNS answer accepted")
	}
}

func TestDiscoveryCannotProxyPrivateTokenEndpoint(t *testing.T) {
	var issuer string
	provider := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": "https://127.0.0.1:9/token",
			"jwks_uri": "https://127.0.0.1:9/keys",
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"]
		}`, issuer, issuer+"/auth")
	}))
	_, port, err := net.SplitHostPort(provider.Listener.Addr().String())
	if err != nil {
		provider.Close()
		t.Fatal(err)
	}
	issuer = "https://93.184.216.34:" + port
	provider.StartTLS()
	defer provider.Close()

	var discoveryConnections, privateConnections atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(request.Host, "127.0.0.1:") {
			privateConnections.Add(1)
			http.Error(w, "private target blocked", http.StatusBadGateway)
			return
		}
		discoveryConnections.Add(1)
		upstream, dialErr := net.Dial("tcp", provider.Listener.Addr().String())
		if dialErr != nil {
			http.Error(w, "provider unavailable", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		downstream, _, hijackErr := hijacker.Hijack()
		if hijackErr != nil {
			_ = upstream.Close()
			return
		}
		_, _ = downstream.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() {
			_, _ = io.Copy(upstream, downstream)
			_ = upstream.Close()
		}()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	policy := EndpointPolicy{
		Proxy: func(*http.Request) (*url.URL, error) { return proxyURL, nil },
		AllowProxyDial: func(_ context.Context, address string) (bool, error) {
			return address == proxyURL.Host, nil
		},
	}
	client := NewProviderClient(policy, time.Minute, time.Now)
	tlsConfig := provider.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsConfig.ServerName = "127.0.0.1"
	client.SetTLSClientConfig(tlsConfig)
	if _, err = client.Discover(t.Context(), issuer); err == nil {
		t.Fatal("private token endpoint must be rejected during discovery")
	}
	if discoveryConnections.Load() != 1 {
		t.Fatalf("discovery connections = %d", discoveryConnections.Load())
	}
	if privateConnections.Load() != 0 {
		t.Fatal("private token target reached the proxy")
	}
}

func TestExchangeCodeRejectsInvalidTokenEndpointBeforeRequest(t *testing.T) {
	client := NewProviderClient(EndpointPolicy{}, time.Minute, time.Now)
	if _, err := client.ExchangeCode(t.Context(), nil, "client", "secret", "https://beat.test/callback", "code", "verifier", nil); err == nil {
		t.Fatal("missing provider info accepted")
	}
	info := &ProviderInfo{TokenURL: "https://127.0.0.1:9/token"}
	if _, err := client.ExchangeCode(t.Context(), info, "client", "secret", "https://beat.test/callback", "code", "verifier", nil); err == nil {
		t.Fatal("private token endpoint accepted")
	}
	info.TokenURL = "http://issuer.example/token"
	if _, err := client.ExchangeCode(t.Context(), info, "client", "secret", "https://beat.test/callback", "code", "verifier", nil); err == nil {
		t.Fatal("plaintext token endpoint accepted")
	}
}
