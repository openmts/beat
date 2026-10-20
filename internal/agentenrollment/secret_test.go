package agentenrollment

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestGrantSecretRoundTripAndConstantTimeMatching(t *testing.T) {
	secret, err := GenerateGrantSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret.Plaintext, GrantSecretPrefix) || len(secret.Hash) != 32 {
		t.Fatalf("unexpected secret shape %q %d", secret.Plaintext, len(secret.Hash))
	}
	if secret.Prefix != GrantSecretPrefix+strings.TrimPrefix(secret.Plaintext, GrantSecretPrefix)[:displayCharacters] {
		t.Fatalf("prefix %q does not mirror the plaintext", secret.Prefix)
	}
	if !SecretMatches(secret.Hash, secret.Plaintext) {
		t.Fatal("generated secret does not match its own hash")
	}
	if SecretMatches(secret.Hash, secret.Plaintext+"x") || SecretMatches(secret.Hash[:8], secret.Plaintext) {
		t.Fatal("mismatched or truncated secret was accepted")
	}
	replayed, err := DescribeSecret(GrantSecretPrefix, secret.Plaintext)
	if err != nil || replayed.Prefix != secret.Prefix || !bytes.Equal(replayed.Hash, secret.Hash) {
		t.Fatalf("describe secret: %v", err)
	}
}

func TestSecretGenerationRejectsForeignPurposeAndShortRandomness(t *testing.T) {
	token, err := GenerateRequestToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token.Plaintext, RequestTokenPrefix) {
		t.Fatalf("request token prefix %q", token.Plaintext)
	}
	ticket, err := GenerateClaimTicket()
	if err != nil || !strings.HasPrefix(ticket.Plaintext, ClaimTicketPrefix) {
		t.Fatalf("claim ticket: %v %q", err, ticket.Plaintext)
	}
	if _, err := DescribeSecret(GrantSecretPrefix, token.Plaintext); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("foreign purpose accepted: %v", err)
	}
	if _, err := DescribeSecret(GrantSecretPrefix, "beat_enroll_v1_short"); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("short body accepted: %v", err)
	}
	if _, err := GenerateGrantSecretFrom(bytes.NewReader([]byte("too short"))); err == nil {
		t.Fatal("truncated randomness was accepted")
	}
	if _, err := GenerateGrantSecretFrom(errorReader{}); err == nil {
		t.Fatal("randomness failure was ignored")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("randomness unavailable") }
