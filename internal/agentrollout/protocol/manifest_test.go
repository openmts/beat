package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func signedFixture(t *testing.T) (SignedManifest, SigningTrust, ed25519.PrivateKey, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, Version: "v2.0.0", Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "root", Artifacts: []Artifact{{ID: "linux", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: 10, SHA256: strings.Repeat("c", 64), SBOMDigest: strings.Repeat("d", 64), ProvenanceDigest: strings.Repeat("e", 64), Support: "supported"}}}
	payload, err := CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return SignedManifest{Payload: payload, Signature: ed25519.Sign(key, payload)}, SigningTrust{KeyID: "root", PublicKey: public, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour)}, key, now
}

func TestPortableManifestVerificationAndTrustBinding(t *testing.T) {
	signed, trust, _, now := signedFixture(t)
	manifest, err := VerifyManifestSignature(signed, trust, now)
	if err != nil || manifest.Version != "v2.0.0" {
		t.Fatal(manifest, err)
	}
	for _, change := range []func(*SigningTrust){func(t *SigningTrust) { t.KeyID = "other" }, func(t *SigningTrust) { t.Revoked = true }, func(t *SigningTrust) { t.PublicKey = nil }, func(t *SigningTrust) { t.NotBefore = now.Add(time.Minute) }, func(t *SigningTrust) { t.ExpiresAt = now }} {
		value := trust
		change(&value)
		if _, err := VerifyManifestSignature(signed, value, now); err == nil {
			t.Fatal("invalid trust accepted")
		}
	}
	altered := signed
	altered.Signature = bytes.Clone(signed.Signature)
	altered.Signature[0] ^= 1
	if _, err := VerifyManifestSignature(altered, trust, now); err == nil {
		t.Fatal("tampered signature accepted")
	}
}

func TestCanonicalPayloadAndFieldValidation(t *testing.T) {
	signed, _, _, now := signedFixture(t)
	original, err := DecodeManifest(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{nil, []byte("{"), append(bytes.Clone(signed.Payload), '\n'), append([]byte(`{"version":"bad",`), signed.Payload[1:]...), append(bytes.Clone(signed.Payload), []byte(" {}")...)} {
		if _, err := DecodeManifest(payload); err == nil {
			t.Fatal("noncanonical JSON accepted")
		}
	}
	changes := []func(*Manifest){
		func(m *Manifest) { m.Version = "dev" }, func(m *Manifest) { m.SchemaVersion = 2 }, func(m *Manifest) { m.Commit = "bad" }, func(m *Manifest) { m.SigningKeyID = "bad/key" }, func(m *Manifest) { m.Channel = "unknown" },
		func(m *Manifest) { m.BuiltAt = now.Add(time.Hour) }, func(m *Manifest) { m.ExpiresAt = now }, func(m *Manifest) { m.Revoked = true }, func(m *Manifest) { m.MinimumServerSchema = 0 }, func(m *Manifest) { m.MaximumProtocol = 0 },
		func(m *Manifest) { m.Artifacts = nil }, func(m *Manifest) { m.Artifacts = append(m.Artifacts, m.Artifacts[0]) },
		func(m *Manifest) { m.Artifacts[0].Size = 0 }, func(m *Manifest) { m.Artifacts[0].SHA256 = "invalid" }, func(m *Manifest) { m.Artifacts[0].OS = "unsupported" }, func(m *Manifest) { m.Artifacts[0].Kind = "script" }, func(m *Manifest) { m.Artifacts[0].Support = "claimed" }, func(m *Manifest) { m.Artifacts[0].Filename = "../agent" },
	}
	for _, change := range changes {
		manifest := original
		manifest.Artifacts = append([]Artifact(nil), original.Artifacts...)
		change(&manifest)
		if err := ValidateManifest(manifest, now); err == nil {
			t.Fatal("invalid field accepted")
		}
	}
	if !ValidSHA256(strings.Repeat("a", 64)) || ValidSHA256("bad") || !ValidSigningKeyID("release-key") || ValidSigningKeyID("../key") {
		t.Fatal("invalid identity/digest validation")
	}
	if len(ManifestDigest(signed.Payload)) != 64 {
		t.Fatal("manifest digest")
	}
}

func TestTransitionProofIsCanonicalAndCannotForgeNextKey(t *testing.T) {
	signed, trust, key, now := signedFixture(t)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	next := KeyTransition{ID: "next", PublicKey: public, NotBefore: now, ExpiresAt: now.Add(time.Hour)}
	payload, err := TransitionSigningBytes(next)
	if err != nil {
		t.Fatal(err)
	}
	next.Signature = ed25519.Sign(key, payload)
	manifest, err := DecodeManifest(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.NextKey = &next
	signed.Payload, err = CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = ed25519.Sign(key, signed.Payload)
	if _, err := VerifyManifestSignature(signed, trust, now); err != nil {
		t.Fatal(err)
	}
	next.Signature[0] ^= 1
	if err := VerifyTransition(trust.PublicKey, &next, now); err == nil {
		t.Fatal("forged transition accepted")
	}
	next.ExpiresAt = now
	if err := VerifyTransition(trust.PublicKey, &next, now); err == nil {
		t.Fatal("expired transition accepted")
	}
}

func TestPortableEntryPointRejectsMalformedExpiredAndInvalidTransition(t *testing.T) {
	signed, trust, key, now := signedFixture(t)
	if _, err := VerifyManifestSignature(SignedManifest{}, trust, now); err != ErrManifest {
		t.Fatal(err)
	}
	manifest, err := DecodeManifest(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ExpiresAt = now
	signed.Payload, err = CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = ed25519.Sign(key, signed.Payload)
	if _, err := VerifyManifestSignature(signed, trust, now); err != ErrManifest {
		t.Fatal(err)
	}
	manifest.ExpiresAt = now.Add(time.Hour)
	manifest.NextKey = &KeyTransition{ID: "next", PublicKey: trust.PublicKey, NotBefore: now, ExpiresAt: now.Add(time.Hour), Signature: make([]byte, ed25519.SignatureSize)}
	signed.Payload, err = CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = ed25519.Sign(key, signed.Payload)
	if _, err := VerifyManifestSignature(signed, trust, now); err != ErrSignature {
		t.Fatal(err)
	}
}
