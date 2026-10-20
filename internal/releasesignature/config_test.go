package releasesignature

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigurationRequiresPinnedTrustAndSafeExecutable(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"relative executable", func(c *Config) { c.Executable = "cosign" }},
		{"missing executable", func(c *Config) { c.Executable += ".missing" }},
		{"missing identity", func(c *Config) { c.CertificateIdentity = "" }},
		{"multiline identity", func(c *Config) { c.CertificateIdentity = "identity\nother" }},
		{"invalid issuer", func(c *Config) { c.OIDCIssuer = "http://issuer.example" }},
		{"issuer credentials", func(c *Config) { c.OIDCIssuer = "https://secret@issuer.example" }},
		{"issuer query", func(c *Config) { c.OIDCIssuer = "https://issuer.example?a=b" }},
		{"invalid root", func(c *Config) { c.TrustedRoot = []byte("not json") }},
		{"missing root", func(c *Config) { c.TrustedRoot = nil }},
		{"large root", func(c *Config) { c.TrustedRoot = make([]byte, maximumRootBytes+1) }},
		{"negative timeout", func(c *Config) { c.Timeout = -time.Second }},
		{"large timeout", func(c *Config) { c.Timeout = maximumDuration + time.Second }},
		{"relative work root", func(c *Config) { c.TemporaryDirectory = "relative" }},
		{"missing work root", func(c *Config) { c.TemporaryDirectory += ".missing" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := verifierFixture(t, "exit 0")
			test.change(&config)
			if _, err := New(config); !errors.Is(err, ErrConfiguration) {
				t.Fatal("unsafe verifier configuration accepted", err)
			}
		})
	}
}

func TestConfigurationRejectsSymlinksAndWritableExecutables(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	link := filepath.Join(config.TemporaryDirectory, "link")
	if err := os.Symlink(config.Executable, link); err != nil {
		t.Fatal(err)
	}
	unsafe := config
	unsafe.Executable = link
	if _, err := New(unsafe); !errors.Is(err, ErrConfiguration) {
		t.Fatal(err)
	}
	if err := os.Chmod(config.Executable, 0o722); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config); !errors.Is(err, ErrConfiguration) {
		t.Fatal(err)
	}
	if err := os.Chmod(config.Executable, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config); !errors.Is(err, ErrConfiguration) {
		t.Fatal(err)
	}
}

func TestConfigurationSnapshotsTrust(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	config.TemporaryDirectory = ""
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.TrustedRoot[0] = 'x'
	if v.config.TrustedRoot[0] != '{' || v.config.Timeout != defaultDuration {
		t.Fatal("verifier trust was mutable")
	}
}

func TestVerifierRejectsUnsafeParentAndUninitializedReceiver(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.TemporaryDirectory, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config); !errors.Is(err, ErrConfiguration) {
		t.Fatal("writable parent accepted", err)
	}
	if err := v.Verify(t.Context(), testEvidence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("changed parent accepted", err)
	}
	if err := os.Chmod(config.TemporaryDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	var missing *Verifier
	if err := missing.Verify(t.Context(), testEvidence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := (&Verifier{}).Verify(t.Context(), testEvidence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
