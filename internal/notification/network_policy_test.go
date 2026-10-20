package notification

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestDispatcherIgnoresProxyEnvironmentAndBoundsResponses(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	for _, large := range []bool{false, true} {
		dispatcher := dispatchFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			if large {
				_, _ = w.Write([]byte(strings.Repeat("x", maximumDeliveryResponseBytes+1)))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		result := dispatcher.Send(t.Context(), testDispatch(TypeWebhook, []byte(`{"endpoint":"https://receiver.example"}`)))
		if large && result.ErrorCode != "response_too_large" || !large && !result.Success {
			t.Fatal("proxy environment or unbounded response changed delivery behavior")
		}
	}
}

func TestDispatcherRefusesCrossOriginAndExcessiveRedirects(t *testing.T) {
	for _, crossOrigin := range []bool{false, true} {
		var calls atomic.Int32
		dispatcher := dispatchFixture(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			target := r.URL.Path + "/next"
			if crossOrigin {
				target = "https://api.telegram.org/unchecked"
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		})
		result := dispatcher.Send(t.Context(), testDispatch(TypeWebhook, []byte(`{"endpoint":"https://receiver.example/start","redirects":3}`)))
		want := int32(4)
		if crossOrigin {
			want = 1
		}
		if result.Success || result.Retryable || result.StatusClass != "3xx" || calls.Load() != want {
			t.Fatal("redirect escaped the reviewed origin or hop budget")
		}
	}
}

func TestRedirectConnectionRevalidatesDNS(t *testing.T) {
	var calls atomic.Int32
	dispatcher := dispatchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
	})
	resolver := &sequentialResolver{answers: []string{"8.8.8.8", "8.8.8.8", "10.0.0.1"}}
	dispatcher.egress.Resolver = resolver
	result := dispatcher.Send(t.Context(), testDispatch(TypeWebhook, []byte(`{"endpoint":"https://receiver.example/start","redirects":1}`)))
	if result.ErrorCode != "egress_denied" || calls.Load() != 1 || resolver.calls != 3 {
		t.Fatal("redirect reused a previously checked DNS answer")
	}
}

func TestCheckedDialUsesOnlyValidatedIPv4AndIPv6Addresses(t *testing.T) {
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")}}
	addresses := []string{}
	egress.Dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		if len(addresses) == 1 {
			return nil, errors.New("PRIVATE_CONNECT_FAILURE")
		}
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	connection, err := egress.DialChecked(t.Context(), DialTarget{Network: "tcp", Address: "receiver.example:443"})
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if strings.Join(addresses, ",") != "8.8.8.8:443,[2606:4700:4700::1111]:443" {
		t.Fatal("checked dial used an unvalidated hostname or address")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	egress.Dial = func(context.Context, string, string) (net.Conn, error) { return nil, ctx.Err() }
	if _, err := egress.DialChecked(ctx, DialTarget{Network: "tcp", Address: "receiver.example:443"}); err == nil {
		t.Fatal("cancelled dial succeeded")
	}
}

func TestNarrowEgressExceptionsAndCorruptPolicyFailClosed(t *testing.T) {
	now := time.Now()
	for _, address := range []string{"10.1.2.3", "fd12:3456:7890::1"} {
		egress := NewEgress()
		prefix := "10.1.2.0/24"
		if strings.Contains(address, ":") {
			prefix = "fd12:3456:7890::/64"
		}
		policy := model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{{CIDR: prefix, Port: 443, Purpose: "local receiver test", ExpiresAt: now.Add(time.Hour)}}}
		if err := ValidateEgressPolicy(policy, now); err != nil {
			t.Fatal(err)
		}
		endpoint := "https://" + net.JoinHostPort(address, "443")
		if err := egress.Validate(t.Context(), endpoint, policy); err != nil {
			t.Fatal("exact narrow CIDR was not honored")
		}
		for _, corrupt := range []string{"0.0.0.0/0", "::/0", "10.1.2.1/24", "not-a-prefix"} {
			policy.Exceptions[0].CIDR = corrupt
			if err := egress.Validate(t.Context(), endpoint, policy); err == nil {
				t.Fatal("invalid stored CIDR policy bypassed address checks")
			}
		}
	}
}
