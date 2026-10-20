package theme

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestMarketDialPinsTheValidatedAddress(t *testing.T) {
	lookups, dials := 0, []string{}
	policy := marketNetwork{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			lookups++
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dials = append(dials, address)
			return nil, errors.New("fixture stop")
		},
	}
	_, err := checkedMarketDial(policy, false)(t.Context(), "tcp", "market.example:443")
	if err == nil || lookups != 1 || len(dials) != 1 || dials[0] != "93.184.216.34:443" {
		t.Fatalf("unbound DNS dial: lookups=%d targets=%v error=%v", lookups, dials, err)
	}
}

func TestMarketDialRejectsMixedAndSpecialAddressSets(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "::ffff:192.168.1.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "2001:db8::1", "2001:2::1", "2001:10::1", "2001:20::1", "3fff::1", "100::1", "::192.168.1.1", "5f00::1", "4000::1", "192.88.99.1", "fec0::1", "64:ff9b::a00:1", "::", "ff02::1"} {
		t.Run(address, func(t *testing.T) {
			dialed := false
			policy := marketNetwork{
				lookup: func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP(address)}}, nil
				},
				dial: func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, errors.New("must not dial")
				},
			}
			if _, err := checkedMarketDial(policy, false)(t.Context(), "tcp", "market.example:443"); !errors.Is(err, ErrMarketInvalid) || dialed {
				t.Fatalf("unsafe dial accepted: %v %v", err, dialed)
			}
		})
	}
}

func TestMarketDialEmptyResolutionCancellationAndFallback(t *testing.T) {
	policy := marketNetwork{lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }, dial: (&net.Dialer{}).DialContext}
	if _, err := checkedMarketDial(policy, false)(t.Context(), "tcp", "bad-address"); err == nil {
		t.Fatal("bad target accepted")
	}
	if _, err := checkedMarketDial(policy, false)(t.Context(), "tcp", "empty.example:443"); err == nil {
		t.Fatal("empty DNS accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	policy.lookup = func(ctx context.Context, _ string) ([]net.IPAddr, error) { return nil, ctx.Err() }
	if _, err := checkedMarketDial(policy, false)(canceled, "tcp", "cancel.example:443"); err == nil {
		t.Fatal("cancellation ignored")
	}
	policy.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("2606:4700::1111")}}, nil
	}
	count := 0
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	policy.dial = func(context.Context, string, string) (net.Conn, error) {
		count++
		if count == 1 {
			return nil, errors.New("unreachable")
		}
		return local, nil
	}
	connection, err := checkedMarketDial(policy, false)(t.Context(), "tcp", "fallback.example:443")
	if err != nil || connection != local || count != 2 {
		t.Fatalf("fallback failed: %v %d", err, count)
	}
}
