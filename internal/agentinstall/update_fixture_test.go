//go:build linux

package agentinstall

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type updateFixture struct {
	base    *fixture
	options UpdateOptions
	data    []byte
	key     ed25519.PrivateKey
}

func newUpdateFixture(t *testing.T) *updateFixture {
	t.Helper()
	f := newFixture(t)
	f.installer.container = func() bool { return false }
	f.installer.version = func() string { return "v1.0.0" }
	for _, path := range []string{f.installer.paths.stateDir, filepath.Dir(f.installer.paths.binary), filepath.Dir(f.installer.paths.unit)} {
		mustMkdir(t, path)
	}
	data := updateELF()
	source, sourceErr := f.installer.executable()
	if sourceErr != nil {
		t.Fatal(sourceErr)
	}
	mustWrite(t, source, data, executableFile)
	mustWrite(t, f.installer.paths.binary, data, executableFile)
	f.result.Config.ServerURL = "https://beat.test"
	writeUpdateJSON(t, f.installer.paths.config, f.result.Config)
	mustWrite(t, f.installer.paths.unit, unitFile(f.installer.paths.binary, f.installer.paths.config), privateFile)
	f.installer.run = func(ctx context.Context, exe string, args []string) (string, error) {
		if args[1] != "show" {
			return f.run(ctx, exe, args)
		}
		f.calls = append(f.calls, append([]string(nil), args...))
		path := f.installer.paths.unit
		if args[2] == UpdateServiceName {
			path = f.installer.helperUnitPath()
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return "FragmentPath=\nDropInPaths=\nLoadState=not-found\n", nil
			}
		}
		return "FragmentPath=" + path + "\nDropInPaths=\nLoadState=loaded\n", nil
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{33}, 32))
	now := time.Now().UTC()
	options := UpdateOptions{ConfigPath: f.installer.paths.config, ManifestPath: filepath.Join(f.installer.paths.stateDir, "manifest.json"),
		TrustPath: filepath.Join(f.installer.paths.stateDir, "trust-input.json"), ArtifactID: "linux", NodeID: "node-id"}
	signed := signedUpdate(t, key, data, "v1.0.0", now)
	writeUpdateJSON(t, options.ManifestPath, signed)
	writeUpdateJSON(t, options.TrustPath, []protocol.SigningTrust{{KeyID: "fixture", PublicKey: key.Public().(ed25519.PublicKey), NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour)}})
	return &updateFixture{base: f, options: options, data: data, key: key}
}

func updateELF() []byte {
	b := make([]byte, 256)
	copy(b, "\x7fELF")
	b[4] = 2
	b[5] = 1
	b[6] = 1
	o := binary.LittleEndian
	o.PutUint16(b[16:], 2)
	o.PutUint16(b[18:], 62)
	if runtime.GOARCH == "arm64" {
		o.PutUint16(b[18:], 183)
	}
	o.PutUint32(b[20:], 1)
	o.PutUint64(b[24:], 0x400080)
	o.PutUint64(b[32:], 64)
	o.PutUint16(b[52:], 64)
	o.PutUint16(b[54:], 56)
	o.PutUint16(b[56:], 1)
	o.PutUint32(b[64:], 1)
	o.PutUint32(b[68:], 5)
	o.PutUint64(b[80:], 0x400000)
	o.PutUint64(b[96:], 256)
	o.PutUint64(b[104:], 256)
	return b
}

func signedUpdate(t *testing.T, key ed25519.PrivateKey, data []byte, version string, now time.Time) protocol.SignedManifest {
	t.Helper()
	digest := sha256.Sum256(data)
	manifest := protocol.Manifest{SchemaVersion: 1, Version: version, Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "fixture",
		Artifacts: []protocol.Artifact{{ID: "linux", OS: "linux", Arch: runtime.GOARCH, Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(key, payload)}
}

func writeUpdateJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, data, privateFile)
}
