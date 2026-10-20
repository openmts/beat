package agentdiagnostics

import (
	"net/netip"

	"github.com/beat/backend/internal/diagnosticprotocol"
)

// admissibleTarget 复核服务端下发的地址：它必须同时落在本地白名单内，且不属于
// 本地明确关闭的类别。服务端解析与本地白名单的交集才是真正执行的目标。
func (settings settings) admissibleTarget(destination string) error {
	address, err := netip.ParseAddr(destination)
	if err != nil {
		return ErrPolicy
	}
	normalized := diagnosticprotocol.NormalizeAddress(address)
	if diagnosticprotocol.HardForbiddenAddress(normalized) || !settings.allowed.allows(normalized) {
		return ErrPolicy
	}
	if diagnosticprotocol.RestrictedAddress(normalized) && !settings.config.AllowPrivateTargets {
		return ErrPolicy
	}
	return nil
}
