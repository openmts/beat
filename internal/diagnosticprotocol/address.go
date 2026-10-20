package diagnosticprotocol

import (
	"net/netip"

	"github.com/beat/backend/internal/model"
)

// 下面是服务端与 Agent 共用的目标地址策略。两份二进制必须使用同一份列表，
// 否则任何一侧的放宽都会让「服务端审批」与「本地白名单」出现漂移。

// hardForbidden 是任何审批都不能放开的类别：未指定、回环、链路本地、组播、
// 保留段以及云元数据端点。
var hardForbidden = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("fe80::/10"),
}

// restricted 是需要显式审批并落在目标白名单内才能使用的窄范围地址。
var restricted = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// HardForbiddenAddress 报告该地址是否属于任何审批都不可放开的类别。
func HardForbiddenAddress(address netip.Addr) bool {
	normalized := NormalizeAddress(address)
	if !normalized.IsValid() {
		return true
	}
	return ContainsPrefix(hardForbidden, normalized)
}

// RestrictedAddress 报告该地址是否属于需要显式 owner 批准的窄范围地址。
func RestrictedAddress(address netip.Addr) bool {
	normalized := NormalizeAddress(address)
	if !normalized.IsValid() {
		return true
	}
	if ContainsPrefix(restricted, normalized) {
		return true
	}
	return normalized.IsPrivate() && !normalized.IsLoopback()
}

// RestrictedPrefix 报告一个已归一化的前缀是否与受限范围重叠。
func RestrictedPrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return true
	}
	for _, candidate := range restricted {
		if prefix.Overlaps(candidate) {
			return true
		}
	}
	return prefix.Addr().IsPrivate()
}

// NormalizeAddress 把 IPv4-mapped 地址归一化成 IPv4，并丢弃无效地址。
func NormalizeAddress(address netip.Addr) netip.Addr {
	if !address.IsValid() {
		return netip.Addr{}
	}
	if address.Is4In6() {
		return address.Unmap()
	}
	return address
}

// ContainsPrefix 报告地址是否落在任一前缀内。
func ContainsPrefix(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// FamilyMatches 报告地址是否属于给定地址族；auto 或未知值匹配任何地址。
func FamilyMatches(address netip.Addr, family string) bool {
	switch family {
	case model.IPFamilyIPv4:
		return address.Is4()
	case model.IPFamilyIPv6:
		return address.Is6() && !address.Is4()
	default:
		return true
	}
}
