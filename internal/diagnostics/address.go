package diagnostics

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

// ErrDestinationDenied 表示目标地址不在任何已批准范围内。
var ErrDestinationDenied = errors.New("diagnostic destination is not approved")

// HardForbiddenAddress 与 RestrictedAddress 复用共享的地址策略，保证服务端审批与
// Agent 本地白名单使用同一份禁列与限列。
func HardForbiddenAddress(address netip.Addr) bool {
	return diagnosticprotocol.HardForbiddenAddress(address)
}

// RestrictedAddress 报告该地址是否属于需要显式 owner 批准的窄范围地址。
func RestrictedAddress(address netip.Addr) bool {
	return diagnosticprotocol.RestrictedAddress(address)
}

// ParseApprovedPrefixes 解析目标记录里的精确 CIDR/地址集合。
func ParseApprovedPrefixes(entries []string) ([]netip.Prefix, error) {
	if len(entries) > model.MaximumDiagnosticTargetAddresses {
		return nil, errors.New("diagnostic allowlist is too long")
	}
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			if address, addressErr := netip.ParseAddr(entry); addressErr == nil {
				prefix = netip.PrefixFrom(address, address.BitLen())
			} else {
				return nil, fmt.Errorf("diagnostic allowlist entry %q is invalid", entry)
			}
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// AddressApproval 描述一次目标地址审批所需的上下文。
type AddressApproval struct {
	PrivateAllowed bool
	Family         string
	Approved       []netip.Prefix
}

// ApproveAddress 校验最终要发送流量的地址是否被本次审批覆盖。
func ApproveAddress(address netip.Addr, approval AddressApproval) error {
	normalized := normalizeAddress(address)
	if !normalized.IsValid() || HardForbiddenAddress(normalized) {
		return ErrDestinationDenied
	}
	if !familyMatches(normalized, approval.Family) {
		return ErrDestinationDenied
	}
	if RestrictedAddress(normalized) && !approval.PrivateAllowed {
		return ErrDestinationDenied
	}
	if len(approval.Approved) > 0 && !containsPrefix(approval.Approved, normalized) {
		return ErrDestinationDenied
	}
	return nil
}

func normalizeAddress(address netip.Addr) netip.Addr {
	return diagnosticprotocol.NormalizeAddress(address)
}

func containsPrefix(prefixes []netip.Prefix, address netip.Addr) bool {
	return diagnosticprotocol.ContainsPrefix(prefixes, address)
}

func familyMatches(address netip.Addr, family string) bool {
	return diagnosticprotocol.FamilyMatches(address, family)
}
