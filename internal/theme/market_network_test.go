package theme

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
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

func TestMarketDialAllowsOnlyExplicitProxyEndpoint(t *testing.T) {
	lookups := 0
	var dialed string
	policy := marketNetwork{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			lookups++
			return nil, errors.New("proxy endpoint must bypass DNS policy")
		},
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("fixture stop")
		},
	}
	allow := func(_ context.Context, address string) (bool, error) { return address == "127.0.0.1:7890", nil }
	if _, err := checkedMarketDialWithProxy(policy, false, allow)(t.Context(), "tcp", "127.0.0.1:7890"); err == nil || lookups != 0 || dialed != "127.0.0.1:7890" {
		t.Fatalf("configured proxy bypass failed: lookups=%d dial=%q err=%v", lookups, dialed, err)
	}
	if _, err := checkedMarketDialWithProxy(policy, false, allow)(t.Context(), "tcp", "127.0.0.1:7891"); !errors.Is(err, ErrMarketUnavailable) || lookups != 1 {
		t.Fatalf("foreign endpoint reached network: lookups=%d err=%v", lookups, err)
	}
}

func TestMarketProxyRejectsPrivateTargetBeforeConnecting(t *testing.T) {
	var reached atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	target, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewMarketClientWithOptions(false, time.Now, MarketOptions{
		Proxy: func(*http.Request) (*url.URL, error) { return target, nil },
		AllowProxyDial: func(context.Context, string) (bool, error) {
			return true, nil
		},
	})
	if _, _, err := client.DownloadPackage(t.Context(), "https://localhost/theme.zip"); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("private proxied target = %v", err)
	}
	if reached.Load() {
		t.Fatal("private target reached the configured proxy")
	}
}

func TestProxiedMarketTargetValidation(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		target, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	public := []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}
	for name, testCase := range map[string]struct {
		target       *url.URL
		allowPrivate bool
		addresses    []net.IPAddr
		lookupErr    error
		want         error
	}{
		"public":          {target: parse("https://market.example/catalog"), addresses: public},
		"public literal":  {target: parse("https://8.8.8.8/catalog")},
		"private allowed": {target: parse("https://internal.example/catalog"), allowPrivate: true, lookupErr: errors.New("lookup must be skipped")},
		"private DNS":     {target: parse("https://internal.example/catalog"), addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, want: ErrMarketInvalid},
		"mixed DNS":       {target: parse("https://market.example/catalog"), addresses: append(public, net.IPAddr{IP: net.ParseIP("10.0.0.1")}), want: ErrMarketInvalid},
		"zoned DNS":       {target: parse("https://market.example/catalog"), addresses: []net.IPAddr{{IP: net.ParseIP("fe80::1"), Zone: "eth0"}}, want: ErrMarketInvalid},
		"empty DNS":       {target: parse("https://market.example/catalog"), want: ErrMarketInvalid},
		"too many DNS":    {target: parse("https://market.example/catalog"), addresses: make([]net.IPAddr, maximumMarketAddresses+1), want: ErrMarketInvalid},
		"lookup error":    {target: parse("https://market.example/catalog"), lookupErr: errors.New("fixture"), want: ErrMarketUnavailable},
		"private literal": {target: parse("https://127.0.0.1/catalog"), want: ErrMarketInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			lookup := func(context.Context, string) ([]net.IPAddr, error) {
				return testCase.addresses, testCase.lookupErr
			}
			err := validateProxiedMarketTarget(t.Context(), testCase.target, testCase.allowPrivate, lookup)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
