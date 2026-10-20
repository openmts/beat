package model

import "testing"

func TestFormatPublicAddressModes(t *testing.T) {
	cases := []struct {
		name  string
		value string
		mode  string
		want  string
	}{
		{"full keeps the raw value", "203.0.113.10", PublicAddressFull, "203.0.113.10"},
		{"full keeps a hostname", "edge.example.com", PublicAddressFull, "edge.example.com"},
		{"hidden always empties", "203.0.113.10", PublicAddressHidden, ""},
		{"unknown mode behaves as hidden", "203.0.113.10", "", ""},
		{"ipv4 keeps the first octet", "203.0.113.10", PublicAddressMasked, "203.*.*.*"},
		{"ipv4 mapped ipv6 is masked as ipv4", "::ffff:203.0.113.10", PublicAddressMasked, "203.*.*.*"},
		{"ipv6 keeps 32 bits", "2001:db8::1", PublicAddressMasked, "2001:0db8::/32"},
		{"ipv6 with port keeps 32 bits", "[2001:db8::1]:9100", PublicAddressMasked, "2001:0db8::/32"},
		{"hostname is not an address", "edge.example.com", PublicAddressMasked, ""},
		{"empty stays empty", "  ", PublicAddressMasked, ""},
		{"invalid stays hidden", "not:an:address", PublicAddressMasked, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := FormatPublicAddress(testCase.value, testCase.mode); got != testCase.want {
				t.Fatalf("FormatPublicAddress(%q, %q) = %q, want %q", testCase.value, testCase.mode, got, testCase.want)
			}
		})
	}
}

func TestValidPublicAddressMode(t *testing.T) {
	for _, mode := range PublicAddressModes() {
		if !ValidPublicAddressMode(mode) {
			t.Fatalf("expected %q to be valid", mode)
		}
	}
	for _, mode := range []string{"", "public", "HIDDEN", "true"} {
		if ValidPublicAddressMode(mode) {
			t.Fatalf("expected %q to be rejected", mode)
		}
	}
}
