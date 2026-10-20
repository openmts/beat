package secretbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDataKeyValidationAndRedaction(t *testing.T) {
	root := newTestManager(t)
	key, err := GenerateDataKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.RootFingerprint()) != 64 {
		t.Fatal("root fingerprint is not canonical")
	}
	encoded, err := json.Marshal(key)
	if err != nil || string(encoded) != "{}" || fmt.Sprint(key) != "application data key" {
		t.Fatal("data key was exposed")
	}
	if fmt.Sprintf("%#v", key) != "application data key" {
		t.Fatal("debug formatting exposed data key")
	}
	wrappedKey := WrappedDataKey{ID: key.ID, Generation: 1, Wrapped: key.Material}
	if fmt.Sprint(wrappedKey) != "wrapped application data key" || fmt.Sprintf("%#v", wrappedKey) != "wrapped application data key" {
		t.Fatal("debug formatting exposed wrapped key")
	}
	for _, invalid := range []DataKey{{ID: "bad", Material: key.Material}, {ID: key.ID, Material: []byte("short")}} {
		if _, err := root.WrapDataKey(invalid, 1); err == nil {
			t.Fatal("invalid data key wrapped")
		}
	}
	if _, err := root.WrapDataKey(key, 0); err == nil {
		t.Fatal("invalid generation wrapped")
	}
	for _, invalid := range []WrappedDataKey{{ID: "bad", Generation: 1}, {ID: key.ID}, {ID: key.ID, Generation: 1, Wrapped: []byte("bad")}} {
		if _, err := root.UnwrapDataKey(invalid); err == nil {
			t.Fatal("invalid wrapped key opened")
		}
	}
	wrapped, err := root.rootCipher().Seal([]byte("short"), dataKeyAAD(key.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.UnwrapDataKey(WrappedDataKey{ID: key.ID, Generation: 1, Wrapped: wrapped}); err == nil {
		t.Fatal("short material accepted")
	}
	if _, err := GenerateDataKey(bytes.NewReader(make([]byte, dataKeyIDBytes+keyLength))); err == nil {
		t.Fatal("zero identifier generated")
	}
}

func TestEnvelopeIdentityAndProfilesRejectSubstitution(t *testing.T) {
	cipher := testDataCipher(t)
	aad := OIDCProviderAAD("provider", 3)
	sealed, err := cipher.Seal([]byte("secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if !IsV2Envelope(sealed) || IsV2Envelope([]byte("BEATENC1")) || IsV2Envelope(nil) {
		t.Fatal("envelope discriminator failed")
	}
	if _, err := EnvelopeDataKeyID([]byte("bad")); err == nil {
		t.Fatal("malformed envelope has identity")
	}
	zero := bytes.Clone(sealed)
	clear(zero[10:v2LengthOffset])
	if _, err := EnvelopeDataKeyID(zero); err == nil {
		t.Fatal("zero key identity accepted")
	}
	for _, wrong := range []ResourceAAD{{}, OIDCProviderAAD("provider", 4), OIDCTransactionAAD("provider", "3")} {
		if _, err := cipher.Open(sealed, wrong); err == nil {
			t.Fatal("AAD substitution accepted")
		}
	}
	if _, err := cipher.Open(sealed, ResourceAAD{Domain: "beat", Namespace: "n", RowID: strings.Repeat("x", 2048), Field: "f", PayloadVersion: "v1"}); err == nil {
		t.Fatal("oversized AAD accepted")
	}
}
