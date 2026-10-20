package releasemanifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func TestParseSpecAcceptsCanonicalDocument(t *testing.T) {
	document := []byte(`{"version":"9.9.9","commit":"a","channel":"stable","artifacts":[]}`)
	spec, err := ParseSpec(document)
	if err != nil {
		t.Fatalf("canonical document rejected: %v", err)
	}
	if spec.Version != "9.9.9" || spec.Channel != "stable" || spec.Artifacts == nil || len(spec.Artifacts) != 0 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestParseSpecRejectsUnusableDocuments(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"oversized":      []byte(`{"version":"` + string(make([]byte, maximumSpecBytes)) + `"}`),
		"malformed":      []byte(`{"version":`),
		"unknown field":  []byte(`{"version":"9.9.9","extra":true}`),
		"trailing value": []byte(`{"version":"9.9.9"} {"version":"9.9.8"}`),
	}
	for name, document := range cases {
		if _, err := ParseSpec(document); !errors.Is(err, ErrSpec) {
			t.Fatalf("%s: expected ErrSpec, got %v", name, err)
		}
	}
}

func TestParseSpecRejectsDocumentsOverTheByteLimit(t *testing.T) {
	document := make([]byte, maximumSpecBytes+1)
	if _, err := ParseSpec(document); !errors.Is(err, ErrSpec) {
		t.Fatalf("expected ErrSpec for an oversized document, got %v", err)
	}
}

func TestGenerateKeyRoundTripsThroughEncodeSeed(t *testing.T) {
	public, private, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(public) != ed25519.PublicKeySize || len(private) != ed25519.PrivateKeySize {
		t.Fatalf("unexpected key sizes: %d/%d", len(public), len(private))
	}
	encoded, err := EncodeSeed(private)
	if err != nil {
		t.Fatalf("EncodeSeed: %v", err)
	}
	parsed, err := ParsePrivateKey([]byte(encoded + "\n"))
	if err != nil {
		t.Fatalf("ParsePrivateKey(base64 seed): %v", err)
	}
	if !parsed.Equal(private) {
		t.Fatalf("seed round trip changed the key")
	}
	if _, err := EncodeSeed(private[:seedBytes]); !errors.Is(err, ErrSpec) {
		t.Fatalf("EncodeSeed must reject material that is not a full private key")
	}
}

func TestParsePrivateKeyAcceptsEveryDocumentedEncoding(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	seed := private.Seed()
	cases := map[string]string{
		"base64 seed": base64.StdEncoding.EncodeToString(seed),
		"hex seed":    hex.EncodeToString(seed),
		"base64 key":  base64.StdEncoding.EncodeToString(private),
		"hex key":     hex.EncodeToString(private),
	}
	for name, encoded := range cases {
		parsed, err := ParsePrivateKey([]byte("  " + encoded + "  "))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !parsed.Equal(private) {
			t.Fatalf("%s: parsed key differs", name)
		}
	}
}

func TestParsePrivateKeyRejectsUnusableMaterial(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"blank":       "   ",
		"not encoded": "not a key",
		"too short":   base64.StdEncoding.EncodeToString(make([]byte, seedBytes-1)),
		"too long":    base64.StdEncoding.EncodeToString(make([]byte, keyBytes+1)),
		"odd hex":     hex.EncodeToString(make([]byte, seedBytes-1)),
	}
	for name, material := range cases {
		if _, err := ParsePrivateKey([]byte(material)); !errors.Is(err, ErrSpec) {
			t.Fatalf("%s: expected ErrSpec, got %v", name, err)
		}
	}
}
