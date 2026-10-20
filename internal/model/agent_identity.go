package model

import (
	"crypto/sha256"
	"encoding/hex"
)

func AgentCredentialFingerprint(hash []byte) string {
	if len(hash) != sha256.Size {
		return ""
	}
	fingerprint := sha256.Sum256(hash)
	return hex.EncodeToString(fingerprint[:])
}
