package notification

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestProviderBusinessErrorsAndRetryAfter(t *testing.T) {
	for _, kind := range []string{TypeBark, TypeTelegram, TypeServerChanTurbo, TypeServerChan3} {
		for _, body := range []string{"", `{}`, `{"code":1,"ok":false,"message":"PRIVATE"}`, `{"code":0,"data":{"errno":1}}`} {
			result := providerResponse(responseInput{kind: kind, response: &http.Response{StatusCode: 200, Header: http.Header{}}, body: []byte(body), now: time.Now()})
			if result.Success || result.Retryable || strings.Contains(result.ErrorCode, "PRIVATE") {
				t.Fatal("HTTP 200 business failure accepted")
			}
		}
	}
	result := providerResponse(responseInput{kind: TypeBark, compatibility: "http_2xx", response: &http.Response{StatusCode: 204, Header: http.Header{}}})
	if !result.Success {
		t.Fatal("explicit Bark compatibility rejected")
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, value := range []string{"bad", "-1", "0"} {
		if retryAfter(value, now) != 0 {
			t.Fatal("invalid Retry-After accepted")
		}
	}
	if retryAfter("999999", now) != time.Hour || retryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now) != 30*time.Second {
		t.Fatal("Retry-After not bounded")
	}
}

func TestDispatcherRejectsMalformedInputsAndUnsafeTLS(t *testing.T) {
	dispatcher := NewDispatcher(DispatcherConfig{TLSConfig: &tls.Config{InsecureSkipVerify: true}})
	if dispatcher.Send(t.Context(), testDispatch(TypeWebhook, json.RawMessage(`{}`))).Success {
		t.Fatal("TLS verification disabled")
	}
	dispatcher = NewDispatcher()
	for _, dispatch := range []model.NotificationDispatch{{ChannelType: TypeWebhook, Config: json.RawMessage("invalid")}, testDispatch("unknown", json.RawMessage(`{}`)),
		{ChannelType: TypeWebhook, Config: json.RawMessage(`{"endpoint":"https://receiver.example"}`), TemplateText: "{{env.SECRET}}"}} {
		if dispatcher.Send(t.Context(), dispatch).Success {
			t.Fatal("invalid dispatch accepted")
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("PRIVATE_URL"), transportFailure{code: "blocked", stage: "dns"}} {
		result := transportResult(cause)
		if strings.Contains(result.ErrorCode, "PRIVATE_") {
			t.Fatal("transport detail leaked")
		}
	}
	origin, _ := url.Parse("https://receiver.example/start")
	redirect, _ := http.NewRequest(http.MethodGet, "https://other.example/path", nil)
	if redirectPolicy(3, origin)(redirect, []*http.Request{{}}) != http.ErrUseLastResponse {
		t.Fatal("cross-origin redirect allowed")
	}
}

func TestPolicyChecksEncodedHostsRebindingAndBroadExceptions(t *testing.T) {
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	for _, endpoint := range []string{"file:///etc/passwd", "https://user:secret@receiver.example", "https://receiver.example/#fragment", "http://8.8.8.8", "https://2130706433", "https://0x7f000001"} {
		if err := egress.Validate(t.Context(), endpoint, model.NotificationEgressPolicy{}); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	for _, host := range []string{"", "bad host", "bad..host", "-bad.host", "bad-.host", "localhost%00", "host_underscore"} {
		if _, err := canonicalHost(host); err == nil {
			t.Fatal("ambiguous host accepted")
		}
	}
	if _, err := egress.resolve(t.Context(), "receiver.example:0", model.NotificationEgressPolicy{}); err == nil {
		t.Fatal("zero port accepted")
	}
	egress.Resolver = resolvingStub{}
	if _, err := egress.resolve(t.Context(), "receiver.example:443", model.NotificationEgressPolicy{}); err == nil {
		t.Fatal("empty DNS answer accepted")
	}
	for _, exception := range []model.NotificationEgressException{{Host: "*.example", Port: 443, Purpose: "bad", ExpiresAt: time.Now().Add(time.Hour)},
		{CIDR: "::/0", Port: 443, Purpose: "bad", ExpiresAt: time.Now().Add(time.Hour)}, {Host: "host", CIDR: "10.0.0.0/24", Port: 443, Purpose: "bad", ExpiresAt: time.Now().Add(time.Hour)}} {
		if err := ValidateEgressPolicy(model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{exception}}, time.Now()); err == nil {
			t.Fatal("unsafe exception accepted")
		}
	}
}

func TestNotificationTransportAllowsConfiguredProxyEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	dispatcher := NewDispatcher(DispatcherConfig{AllowProxyDial: func(_ context.Context, address string) (bool, error) {
		return address == listener.Addr().String(), nil
	}})
	transport := dispatcher.transport(model.NotificationEgressPolicy{}, false)
	defer transport.CloseIdleConnections()
	connection, err := transport.DialContext(t.Context(), "tcp", listener.Addr().String())
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
}

func TestNotificationProxyValidatesEveryRequestTarget(t *testing.T) {
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	proxyCalls := 0
	dispatcher := NewDispatcher(DispatcherConfig{
		Egress: egress,
		Proxy: func(*http.Request) (*url.URL, error) {
			proxyCalls++
			return nil, errors.New("proxy fixture")
		},
	})
	transport := dispatcher.transport(model.NotificationEgressPolicy{}, false)
	defer transport.CloseIdleConnections()

	privateRequest := httptest.NewRequest(http.MethodGet, "https://169.254.169.254/latest/meta-data", nil)
	if _, err := transport.Proxy(privateRequest); err == nil {
		t.Fatal("private proxy target accepted")
	}
	if proxyCalls != 0 {
		t.Fatal("private target reached the proxy selector")
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "https://receiver.example/path", nil)
	if _, err := transport.Proxy(publicRequest); err == nil || !strings.Contains(err.Error(), "proxy fixture") {
		t.Fatalf("public target did not reach the proxy: %v", err)
	}
	if proxyCalls != 1 {
		t.Fatalf("proxy calls = %d", proxyCalls)
	}
}
