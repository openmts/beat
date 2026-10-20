package agentrollout

import (
	"crypto/ed25519"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const MaximumArtifactBytes = protocol.MaximumArtifactBytes

func CanonicalManifest(manifest protocol.Manifest) ([]byte, error) {
	return protocol.CanonicalManifest(manifest)
}

func decodeManifest(payload []byte) (protocol.Manifest, error) {
	return protocol.DecodeManifest(payload)
}

func validateManifest(manifest protocol.Manifest, now time.Time) error {
	return protocol.ValidateManifest(manifest, now)
}

func verifyTransition(key ed25519.PublicKey, next *protocol.KeyTransition, now time.Time) error {
	return protocol.VerifyTransition(key, next, now)
}

func releaseDigest(payload []byte) string { return protocol.ManifestDigest(payload) }
