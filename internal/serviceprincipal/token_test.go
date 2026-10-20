package serviceprincipal

import (
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestGenerateProducesDistinctPrefixedTokens(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if first.Plaintext == second.Plaintext || first.Hash == nil || len(first.Hash) != sha256.Size {
		t.Fatal("generated tokens are not distinct 256-bit credentials")
	}
	if !strings.HasPrefix(first.Plaintext, model.ServiceTokenPrefix) || first.Prefix != model.ServiceTokenPrefix+strings.TrimPrefix(first.Plaintext, model.ServiceTokenPrefix)[:model.ServiceTokenDisplayLength] {
		t.Fatal("generated token prefix is invalid")
	}
	if prefix, ok := DisplayPrefix(first.Plaintext); !ok || prefix != first.Prefix {
		t.Fatal("display prefix does not round-trip")
	}
}

func TestGenerateFromPropagatesEntropyFailure(t *testing.T) {
	if _, err := GenerateFrom(errReader{}); err == nil || !strings.Contains(err.Error(), "generate service token") {
		t.Fatal("entropy failure was not reported", err)
	}
}

func TestDisplayPrefixRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "beat_agent_v1_AAAAAAAA", model.ServiceTokenPrefix + "not!base64", model.ServiceTokenPrefix + "YQ"} {
		if _, ok := DisplayPrefix(token); ok {
			t.Fatalf("accepted malformed token %q", token)
		}
	}
}

func TestMatchesRequiresExactDigestLengthAndValue(t *testing.T) {
	token, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(token.Hash, token.Plaintext) {
		t.Fatal("matching token was rejected")
	}
	if Matches(token.Hash, token.Plaintext+"x") {
		t.Fatal("mismatched token was accepted")
	}
	if Matches(token.Hash[:16], token.Plaintext) {
		t.Fatal("truncated digest was accepted")
	}
	if Matches(nil, token.Plaintext) {
		t.Fatal("empty digest was accepted")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

var _ io.Reader = errReader{}
