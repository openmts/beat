package agentenrollment

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

func enrollmentKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

func TestGenerateChallengeFromBoundedReader(t *testing.T) {
	challenge, err := GenerateChallenge()
	if err != nil || len(challenge) != ChallengeSize {
		t.Fatalf("challenge = %d bytes, %v", len(challenge), err)
	}
	second, err := GenerateChallengeFrom(bytes.NewReader(bytes.Repeat([]byte{9}, ChallengeSize)))
	if err != nil || bytes.Equal(challenge, second) {
		t.Fatalf("injected challenge = %v, %v", second, err)
	}
	short, err := GenerateChallengeFrom(bytes.NewReader([]byte{1, 2, 3}))
	if err == nil || short != nil || !strings.Contains(err.Error(), "generate enrollment challenge") {
		t.Fatalf("short reader = %v, %v", short, err)
	}
}

func TestCanonicalBindingsAreLengthDelimited(t *testing.T) {
	expires := time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)
	request := RequestBinding{ChallengeID: "challenge-1", Challenge: []byte{1, 2}, Origin: "beat.example",
		Fingerprint: []byte{3}, Metadata: []byte{4}, ExpiresAt: expires}
	canonical := request.Canonical()
	if !bytes.HasPrefix(canonical, field([]byte(requestDomain))) {
		t.Fatalf("missing request domain: %x", canonical)
	}
	for _, expected := range [][]byte{[]byte("challenge-1"), []byte{1, 2}, []byte("beat.example"),
		[]byte{3}, []byte{4}, []byte("2026-10-16T09:30:00Z")} {
		if !bytes.Contains(canonical, field(expected)) {
			t.Fatalf("missing field %q in %x", expected, canonical)
		}
	}
	shifted := RequestBinding{ChallengeID: "challenge-", Challenge: []byte("1"), Origin: "beat.example",
		Fingerprint: []byte{3}, Metadata: []byte{4}, ExpiresAt: expires}
	if bytes.Equal(canonical, shifted.Canonical()) {
		t.Fatal("field boundaries are ambiguous")
	}
	status := StatusBinding{RequestID: "request-1", Origin: "beat.example", Timestamp: expires, Nonce: []byte{7}}
	if !bytes.HasPrefix(status.Canonical(), field([]byte(statusDomain))) {
		t.Fatalf("missing status domain: %x", status.Canonical())
	}
	claim := ClaimBinding{RequestID: "request-1", NodeID: "node-1", Ticket: "ticket-1", Origin: "beat.example",
		Timestamp: expires, Nonce: []byte{8}}
	claimCanonical := claim.Canonical()
	if !bytes.HasPrefix(claimCanonical, field([]byte(claimDomain))) {
		t.Fatalf("missing claim domain: %x", claimCanonical)
	}
	for _, expected := range [][]byte{[]byte("node-1"), []byte("ticket-1"), []byte("2026-10-16T09:30:00Z")} {
		if !bytes.Contains(claimCanonical, field(expected)) {
			t.Fatalf("missing claim field %q", expected)
		}
	}
}

func TestFormatInstantUsesUTCSeconds(t *testing.T) {
	local := time.Date(2026, 10, 16, 17, 30, 15, 999, time.FixedZone("CST", 8*3600))
	if formatted := formatInstant(local); formatted != "2026-10-16T09:30:15Z" {
		t.Fatalf("formatted = %s", formatted)
	}
}

func TestVerifySignatureRejectsMismatches(t *testing.T) {
	public, private := enrollmentKeyPair(t)
	payload := []byte("canonical payload")
	signature := SignWith(private, payload)
	if err := VerifySignature(public, payload, signature); err != nil {
		t.Fatalf("valid signature = %v", err)
	}
	if err := VerifySignature(public, []byte("tampered"), signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered payload = %v", err)
	}
	if err := VerifySignature(public[:16], payload, signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("short key = %v", err)
	}
	if !strings.Contains(VerifySignature(public, payload, nil).Error(), "signature does not match payload") {
		t.Fatal("missing signature diagnostic")
	}
}

func TestFingerprintAndDigestAreStable(t *testing.T) {
	if Fingerprint([]byte("key")) != sha256.Sum256([]byte("key")) {
		t.Fatal("fingerprint is not the key digest")
	}
	if Digest([]byte("payload")) != sha256.Sum256([]byte("payload")) {
		t.Fatal("digest mismatch")
	}
	if Fingerprint([]byte("key")) == Fingerprint([]byte("other")) {
		t.Fatal("distinct keys shared a fingerprint")
	}
}

// field 复刻规范编码里的长度前缀，供断言与边界消歧使用。
func field(value []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	return append(length[:], value...)
}
