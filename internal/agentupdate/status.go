package agentupdate

import (
	"errors"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

// ErrorCode不返回URL、凭证、nonce、命令输出或本地配置内容。
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrDisabled):
		return "disabled"
	case errors.Is(err, ErrExternalManager):
		return "external_manager"
	case errors.Is(err, ErrManualOnly), errors.Is(err, ErrConfiguration), errors.Is(err, ErrUnsafePath):
		return "manual_only"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, protocol.ErrSignature):
		return "trust_rejected"
	case errors.Is(err, ErrConflict):
		return "conflict"
	default:
		return "update_failed"
	}
}
