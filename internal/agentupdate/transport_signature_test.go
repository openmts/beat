//go:build linux

package agentupdate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/releasesignature"
)

// 仅替代fixture的Sigstore外部证明；Service和Agent仍做真实Ed25519校验。
type transportVerifier struct{}

func (transportVerifier) Verify(_ context.Context, evidence releasesignature.Evidence) error {
	if len(evidence.Payload) == 0 || string(evidence.Bundle) != "private-fixture-bundle" {
		return agentrollout.ErrSignature
	}
	return nil
}

func (transportVerifier) Policy() releasesignature.PolicyMetadata {
	policy := releasesignature.PolicyMetadata{Version: 1, Root: []byte(`{"fixture":"private-root"}`), VerifierSHA256: strings.Repeat("e", 64), ReleaseWorkflow: "https://github.com/example/beat/.github/workflows/release.yml", OIDCIssuer: "https://token.actions.githubusercontent.com"}
	root := sha256.Sum256(policy.Root)
	policy.RootSHA256 = hex.EncodeToString(root[:])
	revision := sha256.Sum256([]byte(strings.Join([]string{"1", policy.RootSHA256, policy.VerifierSHA256, policy.CertificateIdentity, policy.ReleaseWorkflow, policy.OIDCIssuer}, "\x00")))
	policy.Revision = hex.EncodeToString(revision[:])
	return policy
}
