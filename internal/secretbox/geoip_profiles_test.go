package secretbox

import (
	"bytes"
	"testing"
)

func TestGeoIPProfilesBindNodeModeAndFingerprintDomain(t *testing.T) {
	cipher, err := NewDataCipher("0123456789abcdef0123456789abcdef", bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("8.8.8.8")
	encrypted, err := cipher.Seal(content, GeoIPSourceAAD("node", "observed"))
	if err != nil {
		t.Fatal(err)
	}
	for _, aad := range []ResourceAAD{GeoIPSourceAAD("other", "observed"), GeoIPSourceAAD("node", "managed_target"), GeoIPCredentialAAD()} {
		if _, err = cipher.Open(encrypted, aad); err == nil {
			t.Fatal("AAD mismatch accepted")
		}
	}
	fingerprint, err := cipher.GeoIPFingerprint(content)
	if err != nil || !cipher.VerifyGeoIPFingerprint(content, fingerprint) {
		t.Fatal(err)
	}
	operation, err := cipher.OperationFingerprint(content)
	if err != nil || operation == fingerprint {
		t.Fatal("shared fingerprint domain", err)
	}
	if cipher.VerifyGeoIPFingerprint([]byte("1.1.1.1"), fingerprint) {
		t.Fatal("mismatch accepted")
	}
	if _, err = cipher.GeoIPFingerprint(make([]byte, MaximumSecretBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}
