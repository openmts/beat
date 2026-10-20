//go:build linux

package agentupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

var errInjected = errors.New("injected failure")

type fakeManager struct {
	kind     string
	calls    int
	failures int
}

func (m *fakeManager) Kind() string { return m.kind }

func (m *fakeManager) Restart(context.Context) error {
	m.calls++
	if m.failures > 0 {
		m.failures--
		return errInjected
	}
	return nil
}

type fakeFetcher struct {
	data    []byte
	offsets []int64
	fail    bool
}

func (f *fakeFetcher) TrustedServer() string { return "https://beat.test" }

func (f *fakeFetcher) Fetch(_ context.Context, r FetchRequest) (FetchResponse, error) {
	f.offsets = append(f.offsets, r.Offset)
	if f.fail {
		return FetchResponse{}, errInjected
	}
	return FetchResponse{Body: io.NopCloser(bytes.NewReader(f.data[r.Offset : r.Offset+r.Length])), Offset: r.Offset, Length: r.Length, Total: int64(len(f.data))}, nil
}

type fixture struct {
	u          *Updater
	assignment protocol.Assignment
	manager    *fakeManager
	fetcher    *fakeFetcher
	old, next  []byte
	now        time.Time
	key        ed25519.PrivateKey
}

func testELF(size int, marker byte) []byte {
	b := make([]byte, size)
	copy(b, "\x7fELF")
	b[4] = 2
	b[5] = 1
	b[6] = 1
	o := binary.LittleEndian
	o.PutUint16(b[16:], 2)
	o.PutUint16(b[18:], 62)
	o.PutUint32(b[20:], 1)
	o.PutUint64(b[24:], 0x400080)
	o.PutUint64(b[32:], 64)
	o.PutUint16(b[52:], 64)
	o.PutUint16(b[54:], 56)
	o.PutUint16(b[56:], 1)
	o.PutUint32(b[64:], 1)
	o.PutUint32(b[68:], 5)
	o.PutUint64(b[80:], 0x400000)
	o.PutUint64(b[96:], uint64(size))
	o.PutUint64(b[104:], uint64(size))
	b[size-1] = marker
	return b
}

func imageOf(data []byte, version string) Image {
	digest := sha256.Sum256(data)
	return Image{Version: version, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{old: testELF(256, 1), next: testELF(512, 2), now: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, ed25519.SeedSize))
	dir := t.TempDir()
	must(t, os.Chmod(dir, 0o700))
	executable := filepath.Join(dir, "beat-agent")
	must(t, os.WriteFile(executable, f.old, 0o600))
	must(t, os.Chmod(executable, 0o700))
	f.manager = &fakeManager{kind: "systemd"}
	f.fetcher = &fakeFetcher{data: f.next}
	var err error
	f.u, err = New(Config{Enabled: true, Executable: executable, NodeID: "node", ServerURL: "https://beat.test",
		Installed: imageOf(f.old, "v1.0.0"), Keys: []protocol.SigningTrust{{KeyID: "root", PublicKey: f.key.Public().(ed25519.PublicKey), NotBefore: f.now.Add(-time.Hour), ExpiresAt: f.now.Add(3 * time.Hour)}},
		Manager: f.manager, Fetcher: f.fetcher, OS: "linux", Arch: "amd64", AgentProtocol: 1, Now: func() time.Time { return f.now },
		StartupTimeout: time.Minute, ObservationWindow: 2 * time.Second, MaximumObservationGap: time.Second})
	must(t, err)
	f.resign(t, "v2.0.0")
	return f
}

func (f *fixture) resign(t *testing.T, version string) {
	t.Helper()
	image := imageOf(f.next, version)
	artifact := protocol.Artifact{ID: "linux", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: image.Size, SHA256: image.SHA256, SBOMDigest: strings.Repeat("b", 64), ProvenanceDigest: strings.Repeat("c", 64), Support: "supported"}
	manifest := protocol.Manifest{SchemaVersion: 1, Version: version, Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour),
		MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("d", 64), SigningKeyID: "root", Artifacts: []protocol.Artifact{artifact}}
	payload, err := protocol.CanonicalManifest(manifest)
	must(t, err)
	f.assignment = protocol.Assignment{Action: protocol.AssignmentUpgrade, ServerSchema: 14, ID: "assignment", RolloutID: "rollout", NodeID: "node", Nonce: strings.Repeat("e", 64), ExpiresAt: f.now.Add(time.Hour), Artifact: artifact, Manifest: protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(f.key, payload)}}
	f.fetcher.data = f.next
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantError(t *testing.T, want, got error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func wantPhase(t *testing.T, want string, status Status, err error) {
	t.Helper()
	must(t, err)
	if status.Phase != want {
		t.Fatalf("want phase %s, got %+v", want, status)
	}
}

func (f *fixture) checkCurrent(t *testing.T, want []byte) {
	t.Helper()
	got, err := os.ReadFile(f.u.config.Executable)
	must(t, err)
	if !bytes.Equal(want, got) {
		t.Fatal("entrypoint differs")
	}
}

func (f *fixture) open(t *testing.T) *installation {
	t.Helper()
	i, e := openInstallation(f.u.config.Executable, true)
	must(t, e)
	t.Cleanup(i.close)
	return i
}

func (f *fixture) read(t *testing.T) *journal {
	t.Helper()
	i, e := openInstallation(f.u.config.Executable, false)
	must(t, e)
	defer i.close()
	j, e := readJournal(i)
	must(t, e)
	return j
}

func (f *fixture) mutate(t *testing.T, fn func(*journal)) {
	t.Helper()
	i, e := openInstallation(f.u.config.Executable, false)
	must(t, e)
	defer i.close()
	j, e := readJournal(i)
	must(t, e)
	fn(j)
	must(t, saveJournal(i, j))
}

func (f *fixture) crashAt(point string) {
	f.u.fault = func(p string) error {
		if p == point {
			return errInjected
		}
		return nil
	}
}
