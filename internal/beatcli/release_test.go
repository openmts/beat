package beatcli

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasemanifest"
)

const releaseTestDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func releaseFixture(t *testing.T) (string, string, ed25519.PublicKey, string) {
	t.Helper()
	directory := t.TempDir()
	artifact := filepath.Join(directory, "beat-agent-linux-amd64")
	if err := os.WriteFile(artifact, []byte("agent binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	public, private, err := releasemanifest.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := releasemanifest.EncodeSeed(private)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(directory, "release.key")
	if err := os.WriteFile(keyPath, []byte(seed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	spec := releasemanifest.Spec{Version: "9.9.9", Commit: strings.Repeat("a", 40), Channel: "stable",
		BuiltAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), MinimumServerSchema: 1,
		MinimumProtocol: 1, MaximumProtocol: 2, SigningKeyID: "beat-release-2026",
		ReleaseNotes: releasemanifest.DigestRef{Digest: releaseTestDigest},
		Artifacts: []releasemanifest.ArtifactSpec{{ID: "agent-linux-amd64", OS: "linux", Arch: "amd64",
			Kind: "binary", Path: artifact, Entrypoint: "beat-agent", Support: "supported",
			SBOM: releasemanifest.DigestRef{Digest: releaseTestDigest}, Provenance: releasemanifest.DigestRef{Path: artifact}}}}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(directory, "release.json")
	if err := os.WriteFile(specPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return specPath, keyPath, public, directory
}

func TestReleaseSignWritesAVerifiableManifest(t *testing.T) {
	specPath, keyPath, public, directory := releaseFixture(t)
	out := filepath.Join(directory, "signed.json")
	result := runCommand(t, nil, nil, "release", "sign", "--"+specFlag, specPath, "--"+keyFlag, keyPath, "--"+outFlag, out)
	if result.err != nil {
		t.Fatalf("sign: %v (%s)", result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, "manifest 9.9.9 channel=stable artifacts=1 digest=") || !strings.Contains(result.stdout, "wrote "+out) {
		t.Fatalf("stdout = %q", result.stdout)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var signed protocol.SignedManifest
	if err := json.Unmarshal(content, &signed); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.DecodeManifest(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	trust := protocol.SigningTrust{KeyID: manifest.SigningKeyID, PublicKey: public,
		NotBefore: time.Now().UTC().Add(-time.Hour), ExpiresAt: manifest.ExpiresAt}
	if _, err := protocol.VerifyManifestSignature(signed, trust, time.Now().UTC()); err != nil {
		t.Fatalf("signed manifest did not verify: %v", err)
	}
}

func TestReleaseSignRejectsBadInput(t *testing.T) {
	specPath, keyPath, _, directory := releaseFixture(t)
	positional := runCommand(t, nil, nil, "release", "sign", "--"+specFlag, specPath, "--"+keyFlag, keyPath, "--"+outFlag, filepath.Join(directory, "x.json"), "extra")
	if positional.err == nil {
		t.Fatal("positional arguments were accepted")
	}
	emptySpec := filepath.Join(directory, "empty.json")
	if err := os.WriteFile(emptySpec, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	malformedSpec := filepath.Join(directory, "broken.json")
	if err := os.WriteFile(malformedSpec, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	badKey := filepath.Join(directory, "bad.key")
	if err := os.WriteFile(badKey, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, paths := range map[string][2]string{
		"missing spec":   {filepath.Join(directory, "absent.json"), keyPath},
		"empty spec":     {emptySpec, keyPath},
		"malformed spec": {malformedSpec, keyPath},
		"invalid key":    {specPath, badKey},
		"missing key":    {specPath, filepath.Join(directory, "absent.key")},
	} {
		t.Run(name, func(t *testing.T) {
			result := runCommand(t, nil, nil, "release", "sign", "--"+specFlag, paths[0], "--"+keyFlag, paths[1], "--"+outFlag, filepath.Join(directory, "out.json"))
			if result.err == nil {
				t.Fatal("invalid sign input was accepted")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(directory, "out.json")); err == nil {
		t.Fatal("failed sign left an output file")
	}
	failing := runCommand(t, nil, nil, "release", "sign", "--"+specFlag, specPath, "--"+keyFlag, keyPath, "--"+outFlag, filepath.Join(directory, "absent", "out.json"))
	if failing.err == nil {
		t.Fatal("unwritable output path was accepted")
	}
}

func TestReleaseKeygenWritesAgentAndServerTrust(t *testing.T) {
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "release.key")
	trustPath := filepath.Join(directory, "agent-trust.json")
	serverPath := filepath.Join(directory, "server-trust.json")
	notBefore := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	expiresAt := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	result := runCommand(t, nil, nil, "release", "keygen", "--"+keyIDFlag, "beat-release-2026",
		"--"+keyFlag, keyPath, "--"+trustFlag, trustPath, "--"+serverTrustFlag, serverPath,
		"--"+notBeforeFlag, notBefore, "--"+expiresAtFlag, expiresAt)
	if result.err != nil {
		t.Fatalf("keygen: %v (%s)", result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, "id=beat-release-2026") {
		t.Fatalf("stdout = %q", result.stdout)
	}
	for _, path := range []string{keyPath, trustPath, serverPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
	seed, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releasemanifest.ParsePrivateKey(seed); err != nil {
		t.Fatalf("generated seed did not parse: %v", err)
	}
	content, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	var server serverTrustConfig
	if err := json.Unmarshal(content, &server); err != nil {
		t.Fatal(err)
	}
	if server.SchemaVersion != 1 || !server.Enabled || server.SignatureMode != pinnedSignatureMode ||
		len(server.Keys) != 1 || server.Keys[0].ID != "beat-release-2026" || !server.Keys[0].ExpiresAt.After(server.Keys[0].NotBefore) {
		t.Fatalf("server trust config = %+v", server)
	}
	var agentTrust []protocol.SigningTrust
	agentContent, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(agentContent, &agentTrust); err != nil {
		t.Fatal(err)
	}
	if len(agentTrust) != 1 || len(agentTrust[0].PublicKey) != ed25519.PublicKeySize || !agentTrust[0].PublicKey.Equal(server.Keys[0].PublicKey) {
		t.Fatalf("agent trust = %+v", agentTrust)
	}
	again := runCommand(t, nil, nil, "release", "keygen", "--"+keyIDFlag, "beat-release-2026",
		"--"+keyFlag, keyPath, "--"+trustFlag, trustPath, "--"+serverTrustFlag, serverPath)
	if again.err == nil {
		t.Fatal("keygen overwrote existing key material")
	}
}

func TestReleaseKeygenRejectsBadTrustWindows(t *testing.T) {
	directory := t.TempDir()
	base := []string{"release", "keygen", "--" + keyIDFlag, "beat-release-2026",
		"--" + keyFlag, filepath.Join(directory, "release.key"),
		"--" + trustFlag, filepath.Join(directory, "agent-trust.json"),
		"--" + serverTrustFlag, filepath.Join(directory, "server-trust.json")}
	if result := runCommand(t, nil, nil, append(append([]string{}, base...), "extra")...); result.err == nil {
		t.Fatal("positional arguments were accepted")
	}
	for name, extra := range map[string][]string{
		"malformed not-before": {"--" + notBeforeFlag, "yesterday"},
		"malformed expiry":     {"--" + expiresAtFlag, "tomorrow"},
		"inverted window":      {"--" + notBeforeFlag, "2030-01-02T00:00:00Z", "--" + expiresAtFlag, "2030-01-01T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			if result := runCommand(t, nil, nil, append(append([]string{}, base...), extra...)...); result.err == nil {
				t.Fatal("invalid trust window was accepted")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(directory, "release.key")); err == nil {
		t.Fatal("failed keygen left key material behind")
	}
}

func TestReleaseFileHelpersRejectUnsafeInput(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(link); err == nil {
		t.Fatal("symlinked key material was accepted")
	}
	if _, err := readPrivateFile(directory); err == nil {
		t.Fatal("directory was accepted as key material")
	}
	if _, err := readPrivateFile(filepath.Join(directory, "absent")); err == nil {
		t.Fatal("missing file was accepted")
	}
	if encoded := string(encodeJSON(make(chan int))); encoded != "{}\n" {
		t.Fatalf("unsupported JSON value = %q", encoded)
	}
	if err := writeFile(filepath.Join(directory, "absent", "out"), []byte("x"), false); err == nil {
		t.Fatal("unwritable path was accepted")
	}
}

func TestReleaseSignatureSelfCheckRejectsForeignKey(t *testing.T) {
	specPath, keyPath, _, _ := releaseFixture(t)
	document, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := releasemanifest.ParseSpec(document)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manifest, err := releasemanifest.Build(spec, now)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := readPrivateFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	private, err := releasemanifest.ParsePrivateKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := releasemanifest.Sign(payload, private)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := releasemanifest.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySignature(signed, manifest, foreign, now); err == nil {
		t.Fatal("foreign public key verified a signature")
	}
	if err := verifySignature(signed, manifest, private.Public().(ed25519.PublicKey), now); err != nil {
		t.Fatalf("matching public key was rejected: %v", err)
	}
}
