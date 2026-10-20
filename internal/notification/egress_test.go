package notification

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type resolvingStub struct {
	addresses []netip.Addr
	err       error
}

func (stub resolvingStub) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), stub.addresses...), stub.err
}

func TestEgressDeniesUnsafeAddressFamiliesAndPinsSocket(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "fc00::1", "2001:db8::1", "fe80::1", "0.0.0.0"} {
		t.Run(address, func(t *testing.T) {
			egress := NewEgress()
			egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr(address)}}
			if err := egress.Validate(t.Context(), "https://receiver.example/path", model.NotificationEgressPolicy{}); err == nil {
				t.Fatal("unsafe DNS address accepted")
			}
		})
	}
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	called := ""
	egress.Dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		called = address
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	connection, err := egress.DialChecked(t.Context(), DialTarget{Network: "tcp", Address: "receiver.example:443"})
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if called != "8.8.8.8:443" {
		t.Fatal("dial used DNS name rather than checked IP")
	}
}

func TestEgressExceptionsAreNarrowExpiringAndCannotPermitMetadata(t *testing.T) {
	now := time.Now()
	egress := NewEgress()
	egress.Now = func() time.Time { return now }
	policy := model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{{Host: "127.0.0.1", Port: 8080, Purpose: "local receiver", ExpiresAt: now.Add(time.Hour)}}}
	if err := ValidateEgressPolicy(policy, now); err != nil {
		t.Fatal(err)
	}
	if err := egress.Validate(t.Context(), "http://127.0.0.1:8080/test", policy); err != nil {
		t.Fatal(err)
	}
	policy.Exceptions[0].Port = 9090
	if err := egress.Validate(t.Context(), "http://127.0.0.1:8080/test", policy); err == nil {
		t.Fatal("wrong exception port accepted")
	}
	policy.Exceptions[0].Port = 8080
	now = now.Add(2 * time.Hour)
	if err := egress.Validate(t.Context(), "http://127.0.0.1:8080/test", policy); err == nil {
		t.Fatal("expired exception accepted")
	}
	policy.Exceptions[0] = model.NotificationEgressException{CIDR: "10.0.0.0/8", Port: 443, Purpose: "broad", ExpiresAt: now.Add(time.Hour)}
	if err := ValidateEgressPolicy(policy, now); err == nil {
		t.Fatal("broad private exception accepted")
	}
	policy.Exceptions[0] = model.NotificationEgressException{Host: "169.254.169.254", Port: 443, Purpose: "metadata", ExpiresAt: now.Add(time.Hour)}
	if err := egress.Validate(t.Context(), "https://169.254.169.254", policy); err == nil {
		t.Fatal("metadata exception accepted")
	}
	egress.Resolver = resolvingStub{err: errors.New("PRIVATE_DNS_DETAILS")}
	if err := egress.Validate(t.Context(), "https://receiver.example", model.NotificationEgressPolicy{}); err == nil || err.Error() == "PRIVATE_DNS_DETAILS" {
		t.Fatal("DNS failure not sanitized")
	}
}
