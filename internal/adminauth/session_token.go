package adminauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
)

const sessionTokenBytes = 32

func GenerateSessionToken(randomSource io.Reader) (string, []byte, string, error) {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	value := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(randomSource, value); err != nil {
		return "", nil, "", fmt.Errorf("generate administrator session token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(value)
	hash := sha256.Sum256([]byte(raw))
	return raw, hash[:], raw[:8], nil
}

func SessionTokenMatches(raw string, expectedHash []byte) bool {
	hash := sha256.Sum256([]byte(raw))
	return len(expectedHash) == len(hash) && subtle.ConstantTimeCompare(hash[:], expectedHash) == 1
}
