package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8, 2001:db8::/32")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	if !trusted.Contains("10.1.2.3") || !trusted.Contains("2001:db8::1") {
		t.Fatal("configured proxy addresses were not trusted")
	}
	if trusted.Contains("192.0.2.1") {
		t.Fatal("unconfigured proxy address was trusted")
	}
	if trusted.Contains("invalid") {
		t.Fatal("invalid address was trusted")
	}
	if _, err := ParseTrustedProxies("not-a-cidr"); err == nil {
		t.Fatal("invalid proxy CIDR was accepted")
	}
}

func TestRequestContextIgnoresForwardingFromUntrustedPeer(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://internal.example/", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	request.Header.Set("Forwarded", `for=198.51.100.5;proto=https;host=public.example`)
	request.Header.Set("X-Forwarded-For", "198.51.100.6")
	response := httptest.NewRecorder()

	RequestContext(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		info := RequestInfoFrom(request)
		if info.Scheme != "http" || info.Host != "internal.example" || info.ClientIP != "192.0.2.10" {
			t.Fatalf("request info = %#v", info)
		}
	})).ServeHTTP(response, request)
}

func TestRequestContextUsesTrustedForwardedHeader(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://internal.example/", nil)
	request.RemoteAddr = "10.0.0.2:4321"
	request.Header.Set("Forwarded", `for="[2001:db8::5]:8443";proto=https;host="monitor.example"`)
	response := httptest.NewRecorder()

	RequestContext(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		info := RequestInfoFrom(request)
		if info.Scheme != "https" || info.Host != "monitor.example" || info.ClientIP != "2001:db8::5" {
			t.Fatalf("request info = %#v", info)
		}
		if RequestID(request.Context()) == "" {
			t.Fatal("request ID was not assigned")
		}
	})).ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("request ID response header was not set")
	}
}

func TestRequestContextUsesNearestTrustedXForwardedValues(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://internal.example/", nil)
	request.RemoteAddr = "10.0.0.2:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.4, 10.0.0.3")
	request.Header.Set("X-Forwarded-Proto", "http, https")
	request.Header.Set("X-Forwarded-Host", "spoofed.example, monitor.example")
	response := httptest.NewRecorder()

	RequestContext(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		info := RequestInfoFrom(request)
		if info.Scheme != "https" || info.Host != "monitor.example" || info.ClientIP != "198.51.100.4" {
			t.Fatalf("request info = %#v", info)
		}
	})).ServeHTTP(response, request)
}

func TestEffectiveRequestDrivesOriginAndSecurityHeaders(t *testing.T) {
	trusted, err := ParseTrustedProxies("127.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://internal.example/login", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "monitor.example")
	request.Header.Set("Origin", "https://monitor.example")
	response := httptest.NewRecorder()

	RequestContext(trusted)(SecurityHeaders(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if !SameOrigin(request) {
			t.Fatal("trusted proxy same-origin request was rejected")
		}
	}))).ServeHTTP(response, request)
	if response.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS was not emitted for trusted HTTPS forwarding")
	}
}

func TestForwardingParserRejectsInvalidValues(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	if lastHeaderElement("") != "" || lastCommaValue("") != "" {
		t.Fatal("empty forwarding headers produced values")
	}
	if validScheme("ftp") != "" || validHost("user@example.com") != "" || validHost("example.com/path") != "" {
		t.Fatal("invalid scheme or host was accepted")
	}
	for _, value := range []string{"unknown", "_hidden", "not-an-ip"} {
		if parseForwardedIP(value) != "" {
			t.Fatalf("invalid forwarded IP %q was accepted", value)
		}
	}
	if got := forwardedClientIP("invalid, 10.0.0.2", trusted); got != "10.0.0.2" {
		t.Fatalf("all-trusted forwarding chain = %q", got)
	}
	if got := parseRemoteIP("2001:db8::1"); got != "2001:db8::1" {
		t.Fatalf("raw IPv6 remote address = %q", got)
	}
	values := parseForwardedElement(`for="198.51.100.1";proto=https`)
	if values["proto"] != "https" {
		t.Fatalf("forwarded values = %#v", values)
	}
}
