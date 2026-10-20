package theme

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func marketTestIdentity(t *testing.T, public ed25519.PublicKey) string {
	t.Helper()
	identity, err := MarketIdentity(base64.StdEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func marketTestManifestDigest(t *testing.T, archive []byte) string {
	t.Helper()
	validated, err := ValidateArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	return sha256Hex(validated.ManifestBytes)
}
