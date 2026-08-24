package secretbox

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := New(filepath.Join(t.TempDir(), "key.bin"), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func testAAD(rowID string) ResourceAAD {
	return ResourceAAD{
		Domain: "beat", Namespace: "oidc_provider", RowID: rowID,
		Field: "client_secret", Revision: "3", PayloadVersion: "v1",
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	manager := newTestManager(t)
	plaintext := []byte("super-secret-client-secret")

	ciphertext, err := manager.EncryptEnvelope(plaintext, testAAD("provider-1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decrypted, err := manager.DecryptEnvelope(ciphertext, testAAD("provider-1"))
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round trip mismatch: %q", decrypted)
	}
}

func TestEnvelopeAADBindingRejectsSwap(t *testing.T) {
	manager := newTestManager(t)
	plaintext := []byte("secret")

	ciphertext, err := manager.EncryptEnvelope(plaintext, testAAD("provider-1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	for _, mutated := range []ResourceAAD{
		testAAD("provider-2"), // 行替换
		{Domain: "beat", Namespace: "oidc_provider", RowID: "provider-1", Field: "other", Revision: "3", PayloadVersion: "v1"},
		{Domain: "beat", Namespace: "oidc_provider", RowID: "provider-1", Field: "client_secret", Revision: "4", PayloadVersion: "v1"},
		{Domain: "other", Namespace: "oidc_provider", RowID: "provider-1", Field: "client_secret", Revision: "3", PayloadVersion: "v1"},
		{Domain: "beat", Namespace: "oidc_provider", RowID: "provider-1", Field: "client_secret", Revision: "3", PayloadVersion: "v2"},
	} {
		if _, err := manager.DecryptEnvelope(ciphertext, mutated); err == nil {
			t.Fatalf("AAD swap %#v unexpectedly decrypted", mutated)
		}
	}
}

func TestEnvelopeRejectsTamperedCiphertext(t *testing.T) {
	manager := newTestManager(t)
	ciphertext, err := manager.EncryptEnvelope([]byte("payload"), testAAD("provider-1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := manager.DecryptEnvelope(tampered, testAAD("provider-1")); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
}

func TestEnvelopeRejectsCorruptLayout(t *testing.T) {
	manager := newTestManager(t)
	valid, err := manager.EncryptEnvelope([]byte("payload"), testAAD("provider-1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	cases := map[string][]byte{
		"too short":     valid[:20],
		"bad magic":     append([]byte("WRONGMAG"), valid[8:]...),
		"bad version":   append(append([]byte{}, valid[:8]...), valid[8]^0x01, valid[9]),
		"bad algorithm": append(append(append([]byte{}, valid[:9]...), valid[9]^0x01), valid[10:]...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			raw := append([]byte(nil), raw...)
			if _, err := manager.DecryptEnvelope(raw, testAAD("provider-1")); err == nil {
				t.Fatalf("%s: corrupt layout must fail", name)
			}
		})
	}
}

func TestEnvelopeKeyIDMismatchRejected(t *testing.T) {
	first := newTestManager(t)
	second := newTestManager(t)
	ciphertext, err := first.EncryptEnvelope([]byte("payload"), testAAD("provider-1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := second.DecryptEnvelope(ciphertext, testAAD("provider-1")); err == nil {
		t.Fatal("ciphertext sealed with another key must fail")
	}
}

func TestEnvelopeDeterministicAADMarshal(t *testing.T) {
	first, err := testAAD("provider-1").Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := testAAD("provider-1").Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("AAD marshal must be deterministic")
	}
}

func TestEnvelopeAADSizeLimit(t *testing.T) {
	large := ResourceAAD{
		Domain: strings.Repeat("a", 2048), Namespace: "n", RowID: "r",
		Field: "f", Revision: "1", PayloadVersion: "v1",
	}
	if _, err := large.Marshal(); err == nil {
		t.Fatal("oversized AAD must be rejected")
	}
}

func TestLegacyEncryptDecryptStillWorks(t *testing.T) {
	manager := newTestManager(t)
	plaintext := []byte("legacy totp secret")
	ciphertext, err := manager.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decrypted, err := manager.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("legacy round trip mismatch")
	}
}

func TestEnvelopeRejectsOversizedInputs(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.EncryptEnvelope(make([]byte, maxEnvelopeSize+1), testAAD("p1")); err == nil {
		t.Fatal("oversized plaintext must be rejected")
	}
	if _, err := manager.DecryptEnvelope(make([]byte, maxEnvelopeSize+1), testAAD("p1")); err == nil {
		t.Fatal("oversized ciphertext must be rejected")
	}
	// AAD 超限经 EncryptEnvelope 路径同样拒绝
	oversizedAAD := ResourceAAD{
		Domain: strings.Repeat("a", 3000), Namespace: "n", RowID: "r",
		Field: "f", Revision: "1", PayloadVersion: "v1",
	}
	if _, err := manager.EncryptEnvelope([]byte("x"), oversizedAAD); err == nil {
		t.Fatal("oversized aad through envelope must be rejected")
	}
}
