package loginsecurity

import (
	"net/netip"

	"github.com/beat/backend/internal/model"
)

const (
	ipv4PrefixBits = 24
	ipv6PrefixBits = 48
)

// summarizeIPv4 对公网地址截断到 /24，其余分类只保留固定标签。
func summarizeIPv4(address netip.Addr) Source {
	switch {
	case address.IsLoopback():
		return Source{Class: model.SecuritySourceClassLoopback, Family: model.SecuritySourceFamilyIPv4, Prefix: "loopback"}
	case address.IsPrivate():
		return Source{Class: model.SecuritySourceClassPrivate, Family: model.SecuritySourceFamilyIPv4, Prefix: "private"}
	case address.IsLinkLocalUnicast():
		return Source{Class: model.SecuritySourceClassLinkLocal, Family: model.SecuritySourceFamilyIPv4, Prefix: "link_local"}
	default:
		return Source{Class: model.SecuritySourceClassPublic, Family: model.SecuritySourceFamilyIPv4,
			Prefix: netip.PrefixFrom(address, ipv4PrefixBits).Masked().String()}
	}
}

// summarizeIPv6 对公网地址截断到 /48；唯一本地与链路本地按分类标签处理。
func summarizeIPv6(address netip.Addr) Source {
	switch {
	case address.IsLoopback():
		return Source{Class: model.SecuritySourceClassLoopback, Family: model.SecuritySourceFamilyIPv6, Prefix: "loopback"}
	case address.IsPrivate():
		return Source{Class: model.SecuritySourceClassPrivate, Family: model.SecuritySourceFamilyIPv6, Prefix: "unique_local"}
	case address.IsLinkLocalUnicast():
		return Source{Class: model.SecuritySourceClassLinkLocal, Family: model.SecuritySourceFamilyIPv6, Prefix: "link_local"}
	default:
		return Source{Class: model.SecuritySourceClassPublic, Family: model.SecuritySourceFamilyIPv6,
			Prefix: netip.PrefixFrom(address, ipv6PrefixBits).Masked().String()}
	}
}

// Summary 生成通知负载使用的来源摘要；开启完整来源时也只返回截断前缀。
func (source Source) Summary() string {
	if source.Prefix == "" {
		return model.SecuritySourceClassUnknown
	}
	return source.Prefix
}
