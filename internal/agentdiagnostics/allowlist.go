package agentdiagnostics

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

// allowedSet 是解析后的本地白名单；空集合表示不允许任何目标。
type allowedSet struct {
	prefixes []netip.Prefix
}

// validateAllowlistEntry 只接受精确地址或 CIDR；主机名不会在本地解析。
func validateAllowlistEntry(entry string) error {
	prefix, err := parsePrefix(entry)
	if err != nil {
		return fmt.Errorf("%w: allowlist entry %q is invalid", ErrPolicy, entry)
	}
	if prefix.IsValid() && prefix.Masked() != prefix {
		return fmt.Errorf("%w: allowlist entry %q must be masked", ErrPolicy, entry)
	}
	return nil
}

func parsePrefix(entry string) (netip.Prefix, error) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" || len(trimmed) > model.MaximumDiagnosticAddressBytes {
		return netip.Prefix{}, ErrPolicy
	}
	if prefix, err := netip.ParsePrefix(trimmed); err == nil {
		return prefix, nil
	}
	address, err := netip.ParseAddr(trimmed)
	if err != nil {
		return netip.Prefix{}, ErrPolicy
	}
	return netip.PrefixFrom(address, address.BitLen()), nil
}

func newAllowedSet(entries []string) (allowedSet, error) {
	set := allowedSet{prefixes: make([]netip.Prefix, 0, len(entries))}
	for _, entry := range entries {
		prefix, err := parsePrefix(entry)
		if err != nil || !prefix.IsValid() {
			return allowedSet{}, ErrPolicy
		}
		set.prefixes = append(set.prefixes, prefix.Masked())
	}
	if len(set.prefixes) == 0 {
		return allowedSet{}, ErrPolicy
	}
	return set, nil
}

// allows 报告地址是否落在本地白名单内；不使用 DNS，避免解析漂移。
func (set allowedSet) allows(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	normalized := diagnosticprotocol.NormalizeAddress(address)
	for _, prefix := range set.prefixes {
		if prefix.Addr().Is4() != normalized.Is4() {
			continue
		}
		if prefix.Contains(normalized) {
			return true
		}
	}
	return false
}
