package secretbox

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func testDataCipher(t *testing.T) *DataCipher {
	t.Helper()
	cipher, err := NewDataCipher(strings.Repeat("1", 32), bytes.Repeat([]byte{2}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestV2ResourceEnvelopeRoundTripAndStrictIdentity(t *testing.T) {
	cipher := testDataCipher(t)
	aad := TOTPSecretAAD("user-one")
	sealed, err := cipher.Seal([]byte("SECRET"), aad)
	if err != nil {
		t.Fatal(err)
	}
	id, err := EnvelopeDataKeyID(sealed)
	if err != nil || id != strings.Repeat("1", 32) {
		t.Fatalf("key identity: %v", err)
	}
	plain, err := cipher.Open(sealed, aad)
	if err != nil || string(plain) != "SECRET" {
		t.Fatalf("resource round trip: %v", err)
	}
	if _, err := cipher.Open(sealed, TOTPSecretAAD("user-two")); err == nil {
		t.Fatal("resource row swap accepted")
	}
	if _, err := cipher.Open(sealed, SSHPrivateKeyAAD("user-one")); err == nil {
		t.Fatal("resource type swap accepted")
	}
	other, err := NewDataCipher(strings.Repeat("3", 32), bytes.Repeat([]byte{2}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed, aad); err == nil {
		t.Fatal("key ID swap accepted despite equal key material")
	}
}

func TestV2RejectsMalformedLengthVersionAndTrailingBytes(t *testing.T) {
	cipher := testDataCipher(t)
	sealed, err := cipher.Seal([]byte("private-payload"), TOTPSecretAAD("owner"))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func([]byte) []byte{
		func(value []byte) []byte { return value[:10] },
		func(value []byte) []byte { value[0] ^= 1; return value },
		func(value []byte) []byte { value[8]++; return value },
		func(value []byte) []byte { value[9]++; return value },
		func(value []byte) []byte { binary.BigEndian.PutUint32(value[26:30], 1); return value },
		func(value []byte) []byte { return append(value, 0) },
		func(value []byte) []byte { value[len(value)-1] ^= 1; return value },
	} {
		if _, err := cipher.Open(change(bytes.Clone(sealed)), TOTPSecretAAD("owner")); err == nil {
			t.Fatal("malformed envelope accepted")
		}
	}
}

func TestV2CipherAndAADLimits(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("A", 32), strings.Repeat("0", 32), strings.Repeat("a", 31)} {
		if _, err := NewDataCipher(id, bytes.Repeat([]byte{1}, 32), nil); err == nil {
			t.Fatal("invalid key ID accepted")
		}
	}
	if _, err := NewDataCipher(strings.Repeat("1", 32), nil, nil); err == nil {
		t.Fatal("missing data key accepted")
	}
	cipher := testDataCipher(t)
	for _, aad := range []ResourceAAD{{}, {Domain: "other", Namespace: "n", RowID: "id", Field: "f", PayloadVersion: "v1"},
		{Domain: "beat", Namespace: "n", RowID: "id\n", Field: "f", PayloadVersion: "v1"},
		{Domain: "beat", Namespace: "n", RowID: strings.Repeat("a", 2048), Field: "f", PayloadVersion: "v1"}} {
		if _, err := cipher.Seal([]byte("x"), aad); err == nil {
			t.Fatal("invalid AAD accepted")
		}
	}
	if _, err := cipher.Seal(make([]byte, MaximumSecretBytes+1), TOTPSecretAAD("id")); err == nil {
		t.Fatal("oversized plaintext accepted")
	}
	sealed, err := cipher.Seal(make([]byte, MaximumSecretBytes), TOTPSecretAAD("id"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Open(sealed, TOTPSecretAAD("id")); err != nil {
		t.Fatalf("maximum valid plaintext cannot round-trip: %v", err)
	}
}

func TestV2NonceRequiresCompleteEntropy(t *testing.T) {
	cipher, err := NewDataCipher(strings.Repeat("1", 32), bytes.Repeat([]byte{2}, 32), strings.NewReader("short"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Seal([]byte("secret"), TOTPSecretAAD("id")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial nonce accepted: %v", err)
	}
}

func TestWrappedDataKeyBindsIdentityGenerationAndRoot(t *testing.T) {
	root := newTestManager(t)
	key, err := GenerateDataKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := root.WrapDataKey(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := root.UnwrapDataKey(WrappedDataKey{ID: key.ID, Generation: 1, Wrapped: wrapped})
	if err != nil || !bytes.Equal(key.Material, decoded.Material) {
		t.Fatalf("wrapped key: %v", err)
	}
	if _, err := root.UnwrapDataKey(WrappedDataKey{ID: key.ID, Generation: 2, Wrapped: wrapped}); err == nil {
		t.Fatal("wrapping generation swap accepted")
	}
	if _, err := newTestManager(t).UnwrapDataKey(WrappedDataKey{ID: key.ID, Generation: 1, Wrapped: wrapped}); err == nil {
		t.Fatal("wrong root accepted")
	}
	if _, err := GenerateDataKey(strings.NewReader("short")); err == nil {
		t.Fatal("insufficient key entropy accepted")
	}
}
