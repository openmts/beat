package geoip

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAddressScope(t *testing.T) {
	cases := map[string]string{"fec0::1": "invalid", "2001:20::1": "invalid", "4000::1": "invalid", "3ffe::1": "invalid", "8.8.8.8": "public", "2606:4700:4700::1111": "public", "::ffff:8.8.8.8": "public", "10.1.2.3": "private", "172.16.0.1": "private", "192.168.1.1": "private", "fc00::1": "private", "127.0.0.1": "loopback", "::1": "loopback", "169.254.1.1": "link_local", "fe80::1": "link_local", "100.64.0.1": "carrier_grade_nat", "192.0.2.1": "documentation", "198.51.100.1": "documentation", "203.0.113.1": "documentation", "2001:db8::1": "documentation", "3fff::1": "documentation", "198.18.0.1": "benchmark", "2001:2::1": "benchmark", "224.0.0.1": "multicast", "ff02::1": "multicast", "0.0.0.0": "unspecified", "::": "unspecified", "fe80::1%eth0": "invalid", "1.2.3.999": "invalid", "host.example": "invalid", "192.0.0.8": "invalid", "240.1.1.1": "invalid", "64:ff9b::808:808": "invalid", "2002:808:808::1": "invalid"}
	for input, expected := range cases {
		t.Run(input, func(t *testing.T) {
			_, actual := Classify(input)
			if expected != actual {
				t.Fatalf("want %s got %s", expected, actual)
			}
		})
	}
	if ValidScope("unknown") || !ValidScope("benchmark") || ValidCountry("ZZ") || ValidCountry("us") || !ValidCountry("CN") || ValidVisibility("bad") {
		t.Fatal("validation")
	}
}

func TestProviderContracts(t *testing.T) {
	for _, test := range []struct{ provider, body, want string }{
		{"geojs_https", `{"country_code":"US"}`, "US"}, {"ipinfo_https", `{"country":"JP"}`, "JP"}, {"ip_api_pro_https", `{"status":"success","countryCode":"DE"}`, "DE"},
		{"geojs_https", `{`, ""}, {"geojs_https", `{"country_code":"ZZ"}`, ""}, {"ipinfo_https", `{"bogon":true}`, ""}, {"ip_api_pro_https", `{"status":"fail"}`, ""}, {"other", `{}`, ""},
	} {
		t.Run(test.provider+test.body, func(t *testing.T) {
			actual, err := decodeProvider(test.provider, []byte(test.body))
			if test.want != "" && (err != nil || actual != test.want) {
				t.Fatal(actual, err)
			}
			if test.want == "" && err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
	for _, provider := range []string{"geojs_https", "ipinfo_https", "ip_api_pro_https"} {
		endpoint, err := providerURL(provider, "8.8.8.8", "secret")
		if err != nil || !strings.HasPrefix(endpoint, "https://") {
			t.Fatal(endpoint, err)
		}
		if _, err = providerURL(provider, "10.0.0.1", "secret"); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	if _, err := providerURL("ip_api_pro_https", "8.8.8.8", ""); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := providerURL("arbitrary", "8.8.8.8", ""); err != ErrInvalid {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRemoteFailuresAndTransportPolicy(t *testing.T) {
	client := providerClient()
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || client.Timeout != providerTimeout || client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("unsafe transport")
	}
	cases := []struct {
		status int
		body   string
		err    error
	}{{200, `{"country_code":"US"}`, nil}, {429, `PRIVATE`, nil}, {200, strings.Repeat("x", providerResponseLimit+1), nil}, {200, `invalid`, nil}, {0, "", errors.New("PRIVATE_URL")}}
	for index, test := range cases {
		s := &Service{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "get.geojs.io" || r.URL.Scheme != "https" {
				t.Fatal("endpoint")
			}
			if test.err != nil {
				return nil, test.err
			}
			return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
		})}}
		country, err := s.remoteLookup(t.Context(), providerLookup{provider: "geojs_https", source: "8.8.8.8"})
		if index == 0 && (country != "US" || err != nil) {
			t.Fatal(country, err)
		}
		if index > 0 && (err != ErrUnavailable || strings.Contains(err.Error(), "PRIVATE")) {
			t.Fatal(err)
		}
		if _, err = s.remoteLookup(t.Context(), providerLookup{provider: "geojs_https", source: "10.0.0.1"}); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	s := &Service{nextRemote: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.waitRemote(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if retryDelay(99) != 24*time.Hour || retryDelay(-1) != 5*time.Minute || refreshDelay("a") < 30*24*time.Hour {
		t.Fatal("schedule bounds")
	}
}

func TestTargetValidation(t *testing.T) {
	for _, target := range []string{"host.example", "8.8.8.8", "2001:4860::1"} {
		if !validTarget(target) {
			t.Fatal(target)
		}
	}
	for _, target := range []string{"", "https://host.example", "-bad.example", "bad-.example", "a..b", "host@evil", "private/path", "bad host"} {
		if validTarget(target) {
			t.Fatal(target)
		}
	}
	values, err := resolveTarget(t.Context(), "10.0.0.1")
	if err != nil || len(values) != 1 {
		t.Fatal(values, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = resolveTarget(ctx, "must-not-resolve.invalid"); err == nil {
		t.Fatal("canceled DNS")
	}
}
