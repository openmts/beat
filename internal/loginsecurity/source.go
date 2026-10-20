// Package loginsecurity 只做有界、可测试的登录来源与设备归一化，
// 原始地址与原始 User-Agent 不进入事件、通知负载或指标标签。
package loginsecurity

import (
	"net"
	"net/netip"
	"strings"

	"github.com/beat/backend/internal/model"
)

// Source 是隐私受控的来源摘要：分类、地址族与截断前缀。
type Source struct {
	Class  string
	Family string
	Prefix string
}

// SummarizeSource 把请求来源归一化为固定分类，并只保留 /24 或 /48 前缀。
func SummarizeSource(raw string) Source {
	address, ok := parseSource(raw)
	if !ok {
		return Source{Class: model.SecuritySourceClassUnknown, Family: model.SecuritySourceFamilyUnknown, Prefix: "unknown"}
	}
	if address.Is4() || address.Is4In6() {
		return summarizeIPv4(address.Unmap())
	}
	return summarizeIPv6(address)
}

func parseSource(raw string) (netip.Addr, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	if zone := strings.IndexByte(value, '%'); zone >= 0 {
		value = value[:zone]
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.IsValid() {
		return netip.Addr{}, false
	}
	return address, true
}
