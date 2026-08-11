package model

import (
	"testing"
	"time"
)

func TestAgentCredentialStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		node Node
		want string
	}{
		{name: "legacy", node: Node{}, want: AgentCredentialLegacy},
		{name: "active", node: Node{
			AgentTokenHash: []byte("hash"), AgentTokenCreatedAt: &now,
		}, want: AgentCredentialActive},
		{name: "revoked", node: Node{
			AgentTokenHash: []byte("hash"), AgentTokenCreatedAt: &now, AgentTokenRevokedAt: &now,
		}, want: AgentCredentialRevoked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.node.AgentCredentialStatus(); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}
