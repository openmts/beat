package notification

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type sequentialResolver struct {
	mu      sync.Mutex
	answers []string
	calls   int
}

func (resolver *sequentialResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	answer := resolver.answers[min(resolver.calls, len(resolver.answers)-1)]
	resolver.calls++
	return []netip.Addr{netip.MustParseAddr(answer)}, nil
}

func TestDispatcherChecksRebindingAtActualSocket(t *testing.T) {
	cases := []struct{ scheme, first, second, wantCode string }{
		{"https", "8.8.8.8", "10.0.0.1", "egress_denied"},
		{"http", "127.0.0.1", "8.8.8.8", "plaintext_forbidden"},
	}
	for _, test := range cases {
		t.Run(test.scheme, func(t *testing.T) {
			resolver := &sequentialResolver{answers: []string{test.first, test.second}}
			egress := NewEgress()
			egress.Resolver = resolver
			var connections atomic.Int64
			egress.Dial = func(context.Context, string, string) (net.Conn, error) {
				connections.Add(1)
				return nil, errors.New("unexpected unchecked connection")
			}
			dispatch := testDispatch(TypeWebhook, []byte(`{"endpoint":"`+test.scheme+`://receiver.example:8080/path"}`))
			if test.scheme == "http" {
				dispatch.Policy.Exceptions = []model.NotificationEgressException{{CIDR: "127.0.0.0/24", Port: 8080, Purpose: "local test", ExpiresAt: time.Now().Add(time.Hour)}}
			}
			result := NewDispatcher(DispatcherConfig{Egress: egress}).Send(t.Context(), dispatch)
			if result.ErrorCode != test.wantCode || connections.Load() != 0 || resolver.calls != 2 {
				t.Fatalf("DNS rebinding reached a socket: result=%#v dials=%d resolutions=%d", result, connections.Load(), resolver.calls)
			}
		})
	}
}

func TestEgressDeniesTranslationAndReservedIPv6Ranges(t *testing.T) {
	for _, host := range []string{"64:ff9b::7f00:1", "64:ff9b:1::1", "100::1", "fec0::1", "2001:10::1", "2001:20::1", "::127.0.0.1", "192.88.99.1"} {
		t.Run(host, func(t *testing.T) {
			egress := NewEgress()
			if err := egress.Validate(t.Context(), "https://"+net.JoinHostPort(host, "443"), model.NotificationEgressPolicy{}); err == nil {
				t.Fatal("reserved or IP translation range was accepted")
			}
		})
	}
}
