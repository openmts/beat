package agentrollout

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// pinnedFixture 只配置固定 Ed25519 公钥：没有 Sigstore 验证器，用来覆盖离线实例的信任模式。
type pinnedFixture struct {
	service *Service
	actor   Actor
	now     time.Time
	key     ed25519.PrivateKey
	keyID   string
	signed  protocol.SignedManifest
}

func newPinnedFixture(t *testing.T) *pinnedFixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	actor := Actor{UserID: "owner", SessionID: "session", RequestID: "request"}
	admins := store.NewAdminStore(database.DB)
	user := model.AdminUser{ID: actor.UserID, Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(context.Background(), &user); err != nil {
		t.Fatal(err)
	}
	until := now.Add(48 * time.Hour)
	if err := admins.CreateSession(context.Background(), &model.AdminSession{ID: actor.SessionID, UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32), TokenPrefix: "owner", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &pinnedFixture{actor: actor, now: now, key: key, keyID: "offline-root"}
	f.service, err = New(Config{Database: database.DB, Directory: filepath.Join(t.TempDir(), "releases"), Keys: []TrustedKey{{
		ID: f.keyID, PublicKey: public, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(365 * 24 * time.Hour),
	}}, PinnedOnly: true, Enabled: true, ServerSchema: 14, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	f.sign(t, "")
	return f
}

func (f *pinnedFixture) sign(t *testing.T, keyID string) {
	t.Helper()
	digest := sha256.Sum256([]byte("pinned Agent artifact fixture"))
	if keyID == "" {
		keyID = f.keyID
	}
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "2.1.0", Commit: strings.Repeat("a", 40), Channel: "stable",
		BuiltAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(24 * time.Hour), MinimumServerSchema: 14,
		MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: keyID,
		Artifacts: []protocol.Artifact{{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent",
			Entrypoint: "beat-agent", Size: int64(len("pinned Agent artifact fixture")), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64),
			ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.signed = protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(f.key, payload)}
}

func (f *pinnedFixture) storedPolicy(t *testing.T) (string, string, string) {
	t.Helper()
	var document, root, releaseRevision string
	if err := f.service.db.QueryRow(`SELECT policy_json, root_sha256, (SELECT policy_revision FROM agent_release_signature_state WHERE id=1)
		FROM agent_release_signature_policies WHERE revision=(SELECT policy_revision FROM agent_release_signature_state WHERE id=1)`).Scan(&document, &root, &releaseRevision); err != nil {
		t.Fatal(err)
	}
	return document, root, releaseRevision
}

func TestPinnedOnlyTrustAcceptsSignedReleaseWithoutSigstoreEvidence(t *testing.T) {
	f := newPinnedFixture(t)
	release, err := f.service.Import(t.Context(), f.actor, f.signed)
	if err != nil || !release.Verified {
		t.Fatal("pinned Ed25519 release rejected", err, release.Verified)
	}
	document, root, revision := f.storedPolicy(t)
	if !strings.Contains(document, `"mode":"pinned_ed25519"`) || len(root) != 64 || len(revision) != 64 {
		t.Fatal("pinned policy not persisted", document, root, revision)
	}
	var storedRevision string
	if err := f.service.db.QueryRow("SELECT signature_policy_revision FROM agent_releases WHERE id=?", release.ID).Scan(&storedRevision); err != nil || storedRevision != revision {
		t.Fatal("release did not record the pinned policy revision", storedRevision, revision, err)
	}
	if err := f.service.Ready(t.Context()); err != nil {
		t.Fatal("pinned trust is not ready", err)
	}
	status, err := f.service.Status(t.Context(), f.actor)
	if err != nil || !status.Configured || !status.Ready {
		t.Fatal("pinned status", status, err)
	}
}

func TestPinnedOnlyTrustRejectsSigstoreBundleAndUnknownKey(t *testing.T) {
	f := newPinnedFixture(t)
	withBundle := f.signed
	withBundle.SigstoreBundle = []byte("opaque-sigstore-evidence")
	if _, err := f.service.Import(t.Context(), f.actor, withBundle); !errors.Is(err, ErrSignature) {
		t.Fatal("pinned mode accepted Sigstore evidence", err)
	}
	f.sign(t, "not-configured")
	if _, err := f.service.Import(t.Context(), f.actor, f.signed); !errors.Is(err, ErrSignature) {
		t.Fatal("pinned mode accepted an unconfigured signing key", err)
	}
}

func TestPinnedOnlyTrustRejectsSigstoreVerifierConfiguration(t *testing.T) {
	if _, err := New(Config{Database: nil, Directory: "/tmp", PinnedOnly: true, Sigstore: &fixtureVerifier{}, Enabled: true, ServerSchema: 14}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil database must fail before mode validation", err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, err := New(Config{Database: database.DB, Directory: filepath.Join(t.TempDir(), "releases"), PinnedOnly: true, Sigstore: &fixtureVerifier{},
		Keys: []TrustedKey{{ID: "offline-root", PublicKey: public, NotBefore: now, ExpiresAt: now.Add(time.Hour)}}, Enabled: true, ServerSchema: 14}); !errors.Is(err, ErrInvalid) {
		t.Fatal("pinned mode accepted a Sigstore verifier", err)
	}
}

func TestPinnedPolicyRevisionIsDeterministic(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	first, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]TrustedKey{"a": {ID: "a", PublicKey: first, NotBefore: now, ExpiresAt: now.Add(time.Hour)},
		"b": {ID: "b", PublicKey: second, NotBefore: now, ExpiresAt: now.Add(time.Hour)}}
	right := map[string]TrustedKey{"b": left["b"], "a": left["a"]}
	leftSnapshot, err := snapshotPinnedPolicy(left)
	if err != nil {
		t.Fatal(err)
	}
	rightSnapshot, err := snapshotPinnedPolicy(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftSnapshot.revision != rightSnapshot.revision || !bytes.Equal(leftSnapshot.document, rightSnapshot.document) {
		t.Fatal("pinned policy depends on map order")
	}
	delete(right, "b")
	smaller, err := snapshotPinnedPolicy(right)
	if err != nil {
		t.Fatal(err)
	}
	if smaller.revision == leftSnapshot.revision {
		t.Fatal("pinned policy ignores the key set")
	}
	if _, err := snapshotPinnedPolicy(map[string]TrustedKey{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty key set accepted", err)
	}
}
