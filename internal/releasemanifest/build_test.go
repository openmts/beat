package releasemanifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writeArtifact(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write artifact %s: %v", name, err)
	}
	return path
}

func digestOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// validSpec 返回一份满足协议校验的发布描述；产物指向调用方给出的文件，Provenance 走文件摘要路径。
func validSpec(t *testing.T, path string) Spec {
	t.Helper()
	now := time.Now().UTC()
	return Spec{Version: "9.9.9", Commit: strings.Repeat("a", 40), Channel: "stable",
		BuiltAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), MinimumServerSchema: 28,
		MinimumProtocol: 1, MaximumProtocol: 1, SigningKeyID: "beat-release-2026",
		ReleaseNotes: DigestRef{Digest: testDigest},
		Artifacts: []ArtifactSpec{{ID: "agent-linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary",
			Path: path, Entrypoint: "beat-agent", Support: "supported",
			SBOM: DigestRef{Digest: testDigest}, Provenance: DigestRef{Path: path}}}}
}

func TestBuildCompilesArtifactsAndEvidence(t *testing.T) {
	dir := t.TempDir()
	path := writeArtifact(t, dir, "beat-agent-linux-amd64", "agent binary")
	manifest, err := Build(validSpec(t, path), time.Now().UTC())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if manifest.SchemaVersion != protocol.ManifestVersion || manifest.ReleaseNotesDigest != testDigest {
		t.Fatalf("unexpected manifest header: %+v", manifest)
	}
	if !manifest.BuiltAt.Equal(manifest.BuiltAt.UTC()) || !manifest.ExpiresAt.Equal(manifest.ExpiresAt.UTC()) {
		t.Fatalf("timestamps must be normalised to UTC: %+v", manifest)
	}
	artifact := manifest.Artifacts[0]
	if artifact.Size != int64(len("agent binary")) || artifact.SHA256 != digestOf("agent binary") {
		t.Fatalf("artifact payload was not measured: %+v", artifact)
	}
	if artifact.Filename != "beat-agent-linux-amd64" || artifact.ProvenanceDigest != digestOf("agent binary") {
		t.Fatalf("artifact defaults were not applied: %+v", artifact)
	}
	if artifact.SBOMDigest != testDigest {
		t.Fatalf("explicit SBOM digest must win: %+v", artifact)
	}
}

func TestBuildAcceptsAnExplicitFilename(t *testing.T) {
	dir := t.TempDir()
	spec := validSpec(t, writeArtifact(t, dir, "staged", "payload"))
	spec.Artifacts[0].Filename = "beat-agent"
	manifest, err := Build(spec, time.Now().UTC())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if manifest.Artifacts[0].Filename != "beat-agent" {
		t.Fatalf("explicit filename must be kept: %+v", manifest.Artifacts[0])
	}
}

func TestBuildRejectsUnusableEvidence(t *testing.T) {
	dir := t.TempDir()
	path := writeArtifact(t, dir, "artifact", "payload")
	// 每个用例都从一份新的描述出发：Artifacts 是切片，共用一份会让后面的用例读到改过的字段。
	for name, change := range map[string]func(Spec) Spec{
		"missing release notes":  func(spec Spec) Spec { spec.ReleaseNotes = DigestRef{}; return spec },
		"short release notes":    func(spec Spec) Spec { spec.ReleaseNotes = DigestRef{Digest: "abc"}; return spec },
		"missing notes file":     func(spec Spec) Spec { spec.ReleaseNotes = DigestRef{Path: filepath.Join(dir, "absent")}; return spec },
		"invalid sbom digest":    func(spec Spec) Spec { spec.Artifacts[0].SBOM = DigestRef{Digest: "zz"}; return spec },
		"missing provenance ref": func(spec Spec) Spec { spec.Artifacts[0].Provenance = DigestRef{}; return spec },
	} {
		if _, err := Build(change(validSpec(t, path)), time.Now().UTC()); !errors.Is(err, ErrSpec) {
			t.Fatalf("%s: expected ErrSpec, got %v", name, err)
		}
	}
}

func TestBuildRejectsUnusableArtifacts(t *testing.T) {
	dir := t.TempDir()
	empty := writeArtifact(t, dir, "empty", "")
	// 稀疏文件：只声明长度，不真正占盘，足以触发体积上限分支。
	oversized := filepath.Join(dir, "oversized")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatalf("create oversized artifact: %v", err)
	}
	if err := file.Truncate(protocol.MaximumArtifactBytes + 1); err != nil {
		t.Fatalf("truncate oversized artifact: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close oversized artifact: %v", err)
	}
	for name, path := range map[string]string{
		"missing path": "",
		"absent file":  filepath.Join(dir, "absent"),
		"directory":    dir,
		"empty file":   empty,
		"oversized":    oversized,
	} {
		spec := validSpec(t, writeArtifact(t, dir, "artifact", "payload"))
		spec.Artifacts[0].Path = path
		if _, err := Build(spec, time.Now().UTC()); !errors.Is(err, ErrSpec) {
			t.Fatalf("%s: expected ErrSpec, got %v", name, err)
		}
	}
}

func TestBuildRejectsManifestsTheProtocolWouldRefuse(t *testing.T) {
	dir := t.TempDir()
	path := writeArtifact(t, dir, "artifact", "payload")
	now := time.Now().UTC()
	for name, change := range map[string]func(*Spec, time.Time){
		"unknown channel": func(spec *Spec, _ time.Time) { spec.Channel = "nightly" },
		"expired":         func(spec *Spec, now time.Time) { spec.ExpiresAt = now.Add(-time.Minute) },
		"future build":    func(spec *Spec, now time.Time) { spec.BuiltAt = now.Add(time.Hour) },
		"no artifacts":    func(spec *Spec, _ time.Time) { spec.Artifacts = nil },
		"bad version":     func(spec *Spec, _ time.Time) { spec.Version = "v" },
		"short commit":    func(spec *Spec, _ time.Time) { spec.Commit = "abc" },
	} {
		spec := validSpec(t, path)
		change(&spec, now)
		if _, err := Build(spec, now); !errors.Is(err, ErrSpec) {
			t.Fatalf("%s: expected ErrSpec, got %v", name, err)
		}
	}
}

func TestSignProducesAVerifiableSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("canonical manifest")
	signed, err := Sign(payload, private)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if string(signed.Payload) != string(payload) || !ed25519.Verify(public, signed.Payload, signed.Signature) {
		t.Fatalf("signature does not verify: %+v", signed)
	}
	if _, err := Sign(payload, private[:ed25519.SeedSize]); !errors.Is(err, ErrSpec) {
		t.Fatalf("Sign must reject material that is not a full private key")
	}
}
