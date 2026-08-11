package agentcredential

import (
	"bytes"
	"errors"
	"testing"
)

func TestGenerateFrom(t *testing.T) {
	token, err := GenerateFrom(bytes.NewReader(bytes.Repeat([]byte{0x5a}, randomBytes)))
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if token.Plaintext == "" || token.Prefix == "" || len(token.Hash) != 32 {
		t.Fatalf("token = %#v", token)
	}
	if !Matches(token.Hash, token.Plaintext) || Matches(token.Hash, token.Plaintext+"x") {
		t.Fatal("token hash comparison is incorrect")
	}
	if prefix, ok := DisplayPrefix(token.Plaintext); !ok || prefix != token.Prefix {
		t.Fatalf("display prefix = %q, %v", prefix, ok)
	}
}

func TestGenerate(t *testing.T) {
	token, err := Generate()
	if err != nil || token.Plaintext == "" {
		t.Fatalf("generate token = %#v, err=%v", token, err)
	}
}

func TestGenerateFromRejectsReaderFailure(t *testing.T) {
	if _, err := GenerateFrom(errorReader{}); err == nil {
		t.Fatal("expected random reader error")
	}
}

func TestDisplayPrefixRejectsMalformedTokens(t *testing.T) {
	tests := []string{"", "wrong", tokenPrefix + "bad!", tokenPrefix + "YQ"}
	for _, token := range tests {
		t.Run(token, func(t *testing.T) {
			if _, ok := DisplayPrefix(token); ok {
				t.Fatalf("accepted malformed token %q", token)
			}
		})
	}
	if Matches(nil, "token") {
		t.Fatal("invalid hash matched")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random failure") }
