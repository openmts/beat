package releasesignature

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestPolicySnapshotIsImmutableAndContainsNoLocalPaths(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	policy := v.Policy()
	assertPublicPolicy(t, policy, config)
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), config.Executable) || strings.Contains(string(raw), config.TemporaryDirectory) {
		t.Fatal("policy exposed local paths")
	}
	policy.Root[0] = 'x'
	if v.Policy().Root[0] != '{' {
		t.Fatal("returned policy mutated live trust")
	}
	var missing *Verifier
	if missing.Policy().Revision != "" || (&Verifier{}).Policy().Revision != "" {
		t.Fatal("unconfigured verifier advertised trust")
	}
}

func TestPolicyRevisionBindsEveryTrustInputAndVerifierContent(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	initial, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	revision := initial.Policy().Revision
	for _, change := range []func(*Config){
		func(c *Config) { c.TrustedRoot = []byte(`{"different_root":true}`) },
		func(c *Config) { c.OIDCIssuer = "https://other.example" },
		func(c *Config) { c.CertificateIdentity = "https://workflow.example/other" },
		func(c *Config) {
			c.CertificateIdentity = ""
			c.ReleaseWorkflow = "https://github.com/owner/repo/.github/workflows/release.yml"
		},
		func(c *Config) { other := verifierFixture(t, "exit 1"); c.Executable = other.Executable },
	} {
		next := config
		change(&next)
		verifier, err := New(next)
		if err != nil {
			t.Fatal(err)
		}
		if verifier.Policy().Revision == revision {
			t.Fatal("changed trust retained cached-verification revision")
		}
	}
	other := verifierFixture(t, "exit 0")
	portable, err := New(other)
	if err != nil {
		t.Fatal(err)
	}
	if portable.Policy().Revision != revision {
		t.Fatal("identical public trust depended on local paths")
	}
}

func assertPublicPolicy(t *testing.T, policy PolicyMetadata, config Config) {
	t.Helper()
	digest := sha256.Sum256(config.TrustedRoot)
	if policy.RootSHA256 != hex.EncodeToString(digest[:]) || len(policy.Revision) != 64 || len(policy.VerifierSHA256) != 64 || policy.Version != 1 {
		t.Fatal("invalid public trust metadata")
	}
	if !bytes.Equal(policy.Root, config.TrustedRoot) {
		t.Fatal("public root was omitted from backup metadata")
	}
}
