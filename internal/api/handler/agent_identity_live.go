package handler

import (
	"context"
	"time"

	"github.com/beat/backend/internal/model"
)

const agentIdentityCheckEvery = time.Second

func (h *AgentChannelHandler) WithIdentityVerifier(verify func(context.Context, model.AgentIdentity) error) *AgentChannelHandler {
	h.verifyIdentity = verify
	return h
}

func (c *agentWebSocketConn) CredentialBinding() string { return c.binding }

func agentCredentialBinding(identity model.AgentIdentity) string {
	if identity.Mode == model.AgentCredentialLegacy {
		return model.AgentCredentialLegacy
	}
	return identity.Mode + ":" + identity.CredentialFingerprint
}
