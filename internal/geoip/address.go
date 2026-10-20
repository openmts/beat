package geoip

import "net/netip"

var specialScopes = []struct {
	prefix netip.Prefix
	scope  string
}{
	{netip.MustParsePrefix("100.64.0.0/10"), "carrier_grade_nat"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation"},
	{netip.MustParsePrefix("3fff::/20"), "documentation"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmark"},
	{netip.MustParsePrefix("2001:2::/48"), "benchmark"},
	{netip.MustParsePrefix("0.0.0.0/8"), "invalid"},
	{netip.MustParsePrefix("240.0.0.0/4"), "invalid"},
	{netip.MustParsePrefix("192.0.0.0/24"), "invalid"},
	{netip.MustParsePrefix("192.88.99.0/24"), "invalid"},
	{netip.MustParsePrefix("64:ff9b::/96"), "invalid"},
	{netip.MustParsePrefix("64:ff9b:1::/48"), "invalid"},
	{netip.MustParsePrefix("100::/64"), "invalid"},
	{netip.MustParsePrefix("2001::/32"), "invalid"},
	{netip.MustParsePrefix("2002::/16"), "invalid"},
	{netip.MustParsePrefix("fec0::/10"), "invalid"},
	{netip.MustParsePrefix("2001::/23"), "invalid"},
	{netip.MustParsePrefix("3ffe::/16"), "invalid"},
}

func Classify(raw string) (netip.Addr, string) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, "invalid"
	}
	ip = ip.Unmap()
	if scope := baseScope(ip); scope != "" {
		return ip, scope
	}
	for _, entry := range specialScopes {
		if entry.prefix.Contains(ip) {
			return ip, entry.scope
		}
	}
	if !ip.IsGlobalUnicast() || (ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip)) {
		return ip, "invalid"
	}
	return ip, "public"
}

func ValidScope(scope string) bool {
	switch scope {
	case "public", "private", "loopback", "link_local", "carrier_grade_nat", "documentation", "benchmark", "multicast", "unspecified", "invalid":
		return true
	}
	return false
}

func baseScope(ip netip.Addr) string {
	switch {
	case ip.IsUnspecified():
		return "unspecified"
	case ip.IsLoopback():
		return "loopback"
	case ip.IsPrivate():
		return "private"
	case ip.IsLinkLocalUnicast():
		return "link_local"
	case ip.IsMulticast():
		return "multicast"
	}
	return ""
}
