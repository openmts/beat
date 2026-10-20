package model

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// 公共节点地址的展示模式。hidden 是默认值，invalid 地址永不原样透出。
const (
	PublicAddressHidden = "hidden"
	PublicAddressMasked = "masked"
	PublicAddressFull   = "full"
)

// PublicAddressModes 返回供管理与文档使用的固定取值顺序。
func PublicAddressModes() []string {
	return []string{PublicAddressHidden, PublicAddressMasked, PublicAddressFull}
}

// ValidPublicAddressMode 判断展示模式是否为已登记取值。
func ValidPublicAddressMode(mode string) bool {
	for _, candidate := range PublicAddressModes() {
		if candidate == mode {
			return true
		}
	}
	return false
}

// MaskedPublicAddress 只保留可辨认的网络前缀：IPv4 保留首字节，
// IPv6 保留前 32 位。非法地址一律返回空字符串。
func MaskedPublicAddress(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if address := net.ParseIP(trimmed); address != nil {
		if ipv4 := address.To4(); ipv4 != nil {
			return fmt.Sprintf("%d.*.*.*", ipv4[0])
		}
		// net.IP 实现了 Stringer，%x 会先取字符串再做十六进制，因此必须显式编码字节。
		ipv6 := address.To16()
		return fmt.Sprintf("%s:%s::/32", hex.EncodeToString(ipv6[0:2]), hex.EncodeToString(ipv6[2:4]))
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return MaskedPublicAddress(host)
	}
	return ""
}

// FormatPublicAddress 按展示模式渲染公共节点地址。
// hidden 与非法的 masked 输入都返回空字符串，绝不回退到原始值。
func FormatPublicAddress(value string, mode string) string {
	switch mode {
	case PublicAddressFull:
		return value
	case PublicAddressMasked:
		return MaskedPublicAddress(value)
	default:
		return ""
	}
}
