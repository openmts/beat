package notification

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func dispatchFixture(t *testing.T, handler http.HandlerFunc) *Dispatcher {
	t.Helper()
	serverTLS, clientTLS := notificationTestTLS(t)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLS
	server.StartTLS()
	t.Cleanup(server.Close)
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	egress.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	return NewDispatcher(DispatcherConfig{Egress: egress, TLSConfig: clientTLS})
}

func notificationTestTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"receiver.example", "api.telegram.org", "api.day.app", "sctapi.ftqq.com", "serverchan.example", "smtp.example", "localhost"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(parsed)
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func TestHTTPProvidersDispatchOnlyThroughBoundedVerifiedTransport(t *testing.T) {
	cases := []struct{ kind, config, path string }{
		{TypeWebhook, `{"endpoint":"https://receiver.example/hook"}`, "/hook"},
		{TypeTelegram, `{"bot_token":"PRIVATE_BOT","chat_id":"PRIVATE_RECIPIENT"}`, "/botPRIVATE_BOT/sendMessage"},
		{TypeBark, `{"device_key":"PRIVATE_DEVICE"}`, "/push"},
		{TypeServerChanTurbo, `{"send_key":"PRIVATE_SEND_KEY"}`, "/PRIVATE_SEND_KEY.send"},
		{TypeServerChan3, `{"endpoint":"https://serverchan.example/PRIVATE_ENDPOINT","tags":["tag"]}`, "/PRIVATE_ENDPOINT"},
		{TypeCustomHTTP, `{"endpoint":"https://receiver.example/custom","method":"POST","body_mode":"json","body":{"text":"{{text}}","secret":"{{secret.token}}"},"named_secrets":{"token":"PRIVATE_TOKEN"}}`, "/custom"},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			called := false
			dispatcher := dispatchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.URL.Path != test.path || r.Header.Get("X-Beat-Delivery-ID") != "delivery-id" || r.Header.Get("Idempotency-Key") != "delivery-id" {
					t.Error("provider contract mismatch")
				}
				w.Header().Set("Content-Type", "application/json")
				body := `{"ok":true,"code":200}`
				if test.kind == TypeServerChanTurbo || test.kind == TypeServerChan3 {
					body = `{"code":0,"data":{"errno":0}}`
				}
				_, _ = w.Write([]byte(body))
			})
			result := dispatcher.Send(t.Context(), model.NotificationDispatch{DeliveryID: "delivery-id", ChannelType: test.kind, Attempt: 1, Config: json.RawMessage(test.config), Payload: model.NotificationPayload{Subject: "Subject", Text: "Message"}})
			if !result.Success || !called {
				t.Fatalf("provider failed: %#v", result)
			}
		})
	}
}

func TestHTTPRetryAndErrorSecrecy(t *testing.T) {
	for _, status := range []int{400, 401, 403, 408, 425, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			dispatcher := dispatchFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("PRIVATE_UPSTREAM_BODY"))
			})
			result := dispatcher.Send(t.Context(), model.NotificationDispatch{ChannelType: TypeWebhook, Config: json.RawMessage(`{"endpoint":"https://receiver.example/PRIVATE_URL"}`), Payload: model.NotificationPayload{Text: "text"}})
			wantRetry := status == 408 || status == 425 || status == 429 || status >= 500
			encoded, err := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "PRIVATE_") || result.Retryable != wantRetry || result.Success {
				t.Fatalf("unsafe classification: %#v", result)
			}
		})
	}
}

func TestRedirectsDefaultOffAndSameOriginOnly(t *testing.T) {
	for _, redirects := range []int{0, 1} {
		t.Run(string(rune('0'+redirects)), func(t *testing.T) {
			calls := 0
			dispatcher := dispatchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/first" {
					http.Redirect(w, r, "/second", http.StatusTemporaryRedirect)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			config, err := json.Marshal(ProviderConfig{Endpoint: "https://receiver.example/first", Redirects: redirects})
			if err != nil {
				t.Fatal(err)
			}
			result := dispatcher.Send(t.Context(), model.NotificationDispatch{ChannelType: TypeWebhook, Config: config, Payload: model.NotificationPayload{Text: "text"}})
			if result.Success != (redirects == 1) || calls != redirects+1 {
				t.Fatalf("redirect policy: %#v, calls=%d", result, calls)
			}
		})
	}
}
