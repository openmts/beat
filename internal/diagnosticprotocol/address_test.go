package diagnosticprotocol

import (
	"net/netip"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestHardForbiddenAddresses(t *testing.T) {
	for name, address := range map[string]string{
		"unspecified":     "0.0.0.0",
		"loopback":        "127.0.0.1",
		"link local":      "169.254.10.1",
		"multicast":       "224.0.0.1",
		"reserved":        "240.0.0.1",
		"ipv6 any":        "::",
		"ipv6 loopback":   "::1",
		"ipv6 multicast":  "ff02::1",
		"ipv6 link local": "fe80::1",
	} {
		if !HardForbiddenAddress(netip.MustParseAddr(address)) {
			t.Fatalf("%s not forbidden", name)
		}
	}
	if !HardForbiddenAddress(netip.Addr{}) {
		t.Fatal("invalid address must be forbidden")
	}
	for name, address := range map[string]string{
		"public v4":        "8.8.8.8",
		"documentation v4": "192.0.2.5",
		"public v6":        "2001:4860:4860::8888",
		"mapped public v4": "::ffff:8.8.8.8",
	} {
		if HardForbiddenAddress(netip.MustParseAddr(address)) {
			t.Fatalf("%s forbidden", name)
		}
	}
}

func TestRestrictedAddressesAndPrefixes(t *testing.T) {
	for name, address := range map[string]string{
		"rfc1918":        "10.1.2.3",
		"carrier grade":  "100.64.0.1",
		"rfc1918 mid":    "172.16.0.1",
		"home":           "192.168.1.1",
		"benchmark":      "198.18.0.1",
		"documentation":  "198.51.100.7",
		"documentation2": "203.0.113.9",
		"unique local":   "fc00::1",
		"documentation6": "2001:db8::1",
		"mapped private": "::ffff:10.9.9.9",
	} {
		if !RestrictedAddress(netip.MustParseAddr(address)) {
			t.Fatalf("%s not restricted", name)
		}
	}
	for name, address := range map[string]string{"public v4": "9.9.9.9", "public v6": "2001:4860::1"} {
		if RestrictedAddress(netip.MustParseAddr(address)) {
			t.Fatalf("%s restricted", name)
		}
	}
	if !RestrictedAddress(netip.Addr{}) {
		t.Fatal("invalid address must be restricted")
	}
	if !RestrictedPrefix(netip.Prefix{}) {
		t.Fatal("invalid prefix must be restricted")
	}
	if !RestrictedPrefix(netip.MustParsePrefix("10.0.0.0/8")) ||
		!RestrictedPrefix(netip.MustParsePrefix("192.160.0.0/12")) ||
		!RestrictedPrefix(netip.MustParsePrefix("fc00::/7")) {
		t.Fatal("restricted prefixes not detected")
	}
	if RestrictedPrefix(netip.MustParsePrefix("8.8.8.0/24")) {
		t.Fatal("public prefix treated as restricted")
	}
}

func TestAddressNormalisationAndMatching(t *testing.T) {
	mapped := netip.MustParseAddr("::ffff:192.0.2.1")
	if normalized := NormalizeAddress(mapped); !normalized.Is4() || normalized.String() != "192.0.2.1" {
		t.Fatalf("normalized = %s", normalized)
	}
	if NormalizeAddress(netip.Addr{}).IsValid() {
		t.Fatal("invalid address normalised to a value")
	}
	plain := netip.MustParseAddr("2001:db8::1")
	if NormalizeAddress(plain) != plain {
		t.Fatal("ipv6 address changed")
	}
	prefixes := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	if !ContainsPrefix(prefixes, netip.MustParseAddr("192.0.2.9")) || ContainsPrefix(prefixes, netip.MustParseAddr("198.51.100.1")) {
		t.Fatal("prefix matching is wrong")
	}
	if ContainsPrefix(nil, netip.MustParseAddr("192.0.2.9")) {
		t.Fatal("empty list matched")
	}
	v4, v6 := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1")
	cases := []struct {
		name     string
		address  netip.Addr
		family   string
		expected bool
	}{
		{"v4 with v4 family", v4, model.IPFamilyIPv4, true},
		{"v4 with v6 family", v4, model.IPFamilyIPv6, false},
		{"v6 with v6 family", v6, model.IPFamilyIPv6, true},
		{"v6 with v4 family", v6, model.IPFamilyIPv4, false},
		{"auto family", v6, model.IPFamilyAuto, true},
		{"unknown family", v4, "ipx", true},
	}
	for _, testCase := range cases {
		if actual := FamilyMatches(testCase.address, testCase.family); actual != testCase.expected {
			t.Fatalf("%s = %v", testCase.name, actual)
		}
	}
}
