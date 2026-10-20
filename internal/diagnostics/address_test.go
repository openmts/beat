package diagnostics

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestHardForbiddenAddressesNeverPassApproval(t *testing.T) {
	cases := []string{"127.0.0.1", "0.0.0.0", "169.254.169.254", "224.0.0.1", "::1",
		"fe80::1", "ff02::1", "255.255.255.255"}
	for _, value := range cases {
		address := netip.MustParseAddr(value)
		if !HardForbiddenAddress(address) {
			t.Fatalf("address %s was not rejected", value)
		}
		err := ApproveAddress(address, AddressApproval{PrivateAllowed: true,
			Approved: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}})
		if !errors.Is(err, ErrDestinationDenied) {
			t.Fatalf("address %s approval error = %v", value, err)
		}
	}
	if HardForbiddenAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IPv6 address was rejected")
	}
	if HardForbiddenAddress(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public IPv4 address was rejected")
	}
}

func TestRestrictedAddressesRequireOwnerApproval(t *testing.T) {
	cases := []string{"10.1.2.3", "172.16.0.1", "192.168.1.10", "100.64.0.1", "192.0.2.5",
		"198.18.0.9", "198.51.100.7", "203.0.113.7", "fd00::1", "2001:db8::5"}
	for _, value := range cases {
		address := netip.MustParseAddr(value)
		if !RestrictedAddress(address) {
			t.Fatalf("address %s was not treated as restricted", value)
		}
		if err := ApproveAddress(address, AddressApproval{}); !errors.Is(err, ErrDestinationDenied) {
			t.Fatalf("restricted address %s error = %v", value, err)
		}
		approved := AddressApproval{PrivateAllowed: true,
			Approved: []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}}
		if err := ApproveAddress(address, approved); err != nil {
			t.Fatalf("approved restricted address %s error = %v", value, err)
		}
	}
	if err := ApproveAddress(netip.MustParseAddr("1.1.1.1"), AddressApproval{}); err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
}

func TestApproveAddressHonoursPinsAndFamily(t *testing.T) {
	approved := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	pinned := []netip.Prefix{netip.MustParsePrefix("93.184.216.0/24")}
	if err := ApproveAddress(netip.MustParseAddr("93.184.216.34"),
		AddressApproval{Approved: pinned}); err != nil {
		t.Fatalf("pinned address rejected: %v", err)
	}
	if err := ApproveAddress(netip.MustParseAddr("93.184.217.34"),
		AddressApproval{Approved: pinned}); !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("unpinned address error = %v", err)
	}
	if err := ApproveAddress(netip.MustParseAddr("1.1.1.1"),
		AddressApproval{Family: model.IPFamilyIPv6, Approved: approved}); !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("family mismatch error = %v", err)
	}
	if err := ApproveAddress(netip.MustParseAddr("2606:4700::1111"),
		AddressApproval{Family: model.IPFamilyIPv6}); err != nil {
		t.Fatalf("IPv6 address rejected: %v", err)
	}
	mapped := netip.MustParseAddr("::ffff:10.0.0.1")
	if !RestrictedAddress(mapped) {
		t.Fatal("IPv4-mapped restricted address was not restricted")
	}
	if err := ApproveAddress(mapped, AddressApproval{PrivateAllowed: true, Family: model.IPFamilyIPv4,
		Approved: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}); err != nil {
		t.Fatalf("mapped approved address rejected: %v", err)
	}
	if err := ApproveAddress(netip.Addr{}, AddressApproval{}); !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("invalid address error = %v", err)
	}
}

func TestParseApprovedPrefixes(t *testing.T) {
	prefixes, err := ParseApprovedPrefixes([]string{"203.0.113.7", "10.0.0.0/8"})
	if err != nil || len(prefixes) != 2 {
		t.Fatalf("prefixes = %v, %v", prefixes, err)
	}
	if prefixes[0].String() != "203.0.113.7/32" || prefixes[1].String() != "10.0.0.0/8" {
		t.Fatalf("prefixes = %v", prefixes)
	}
	if _, err := ParseApprovedPrefixes([]string{"not-an-address"}); err == nil {
		t.Fatal("invalid entry accepted")
	}
	tooMany := make([]string, model.MaximumDiagnosticTargetAddresses+1)
	for index := range tooMany {
		tooMany[index] = "203.0.113.1"
	}
	if _, err := ParseApprovedPrefixes(tooMany); err == nil {
		t.Fatal("oversized allowlist accepted")
	}
}
