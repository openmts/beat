package agentrollouthttp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/releasesignature"
	"github.com/beat/backend/internal/store"
)

type fixtureVerifier struct {
	policy *releasesignature.PolicyMetadata
	bundle string
	err    error
	calls  atomic.Int64
}

func (v *fixtureVerifier) Verify(_ context.Context, e releasesignature.Evidence) error {
	v.calls.Add(1)
	expected := v.bundle
	if expected == "" {
		expected = "fixture-bundle"
	}
	if len(e.Payload) == 0 || string(e.Bundle) != expected {
		return agentrollout.ErrSignature
	}
	return v.err
}

type fixture struct {
	s          *agentrollout.Service
	db         *store.SQLiteStore
	actor      agentrollout.Actor
	now        time.Time
	key        ed25519.PrivateKey
	signed     protocol.SignedManifest
	artifact   []byte
	identities map[string]model.AgentIdentity
	verifier   *fixtureVerifier
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	f := &fixture{db: database, now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), actor: agentrollout.Actor{UserID: "owner", SessionID: "session", RequestID: "request"}, artifact: []byte("verified Agent artifact fixture"), identities: map[string]model.AgentIdentity{}, verifier: &fixtureVerifier{}}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.key = key
	f.seedOwner(t)
	f.s, err = agentrollout.New(agentrollout.Config{Database: database.DB, Directory: filepath.Join(t.TempDir(), "releases"), Keys: []agentrollout.TrustedKey{{ID: "root", PublicKey: public, NotBefore: f.now.Add(-time.Hour), ExpiresAt: f.now.Add(365 * 24 * time.Hour)}}, Sigstore: f.verifier, Enabled: true, ServerSchema: 14, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.artifact)
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "2.0.0", Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(24 * time.Hour), MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "root", Artifacts: []protocol.Artifact{{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: int64(len(f.artifact)), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	f.sign(t, manifest)
	return f
}

func (f *fixture) seedOwner(t *testing.T) {
	t.Helper()
	admins := store.NewAdminStore(f.db.DB)
	user := model.AdminUser{ID: f.actor.UserID, Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "fixture", PasswordChangedAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	until := f.now.Add(48 * time.Hour)
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: f.actor.SessionID, UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32), TokenPrefix: "owner", CreatedAt: f.now, LastActivityAt: f.now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) sign(t *testing.T, manifest protocol.Manifest) {
	t.Helper()
	payload, err := agentrollout.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.signed = protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(f.key, payload), SigstoreBundle: []byte("fixture-bundle")}
}

func (f *fixture) nodes(t *testing.T, count int) []string {
	t.Helper()
	ids := []string{}
	for index := range count {
		hash := bytes.Repeat([]byte{byte(index + 2)}, 32)
		node, err := store.NewNodeStore(f.db.DB).CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "node-" + string(rune('a'+index)), Host: "127.0.0.1", Port: 22}, store.AgentCredential{Hash: hash, Prefix: "fixture", CreatedAt: f.now})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.DB.Exec("UPDATE nodes SET agent_version='1.0.0',os='linux',arch='amd64',last_seen=?,tags='[\"canary\"]' WHERE id=?", f.now, node.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, node.ID)
		f.identities[node.ID] = model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(hash)}
	}
	return ids
}

func (f *fixture) release(t *testing.T) agentrollout.Release {
	t.Helper()
	release, err := f.s.Import(t.Context(), f.actor, f.signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.PutArtifact(t.Context(), f.actor, agentrollout.ArtifactUpload{ReleaseID: release.ID, ArtifactID: "linux-amd64", Body: bytes.NewReader(f.artifact)}); err != nil {
		t.Fatal(err)
	}
	return release
}

func (f *fixture) rollout(t *testing.T, count int) (agentrollout.Rollout, []agentrollout.Target) {
	t.Helper()
	ids := f.nodes(t, count)
	release := f.release(t)
	rollout, err := f.s.Create(t.Context(), f.actor, agentrollout.CreateInput{Name: "Canary release", ReleaseID: release.ID, NodeIDs: ids, Policy: agentrollout.Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	_, targets, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil {
		t.Fatal(err)
	}
	return rollout, targets
}

func capabilities() protocol.Capabilities {
	return protocol.Capabilities{CurrentVersion: "1.0.0", OS: "linux", Arch: "amd64", Manager: "systemd", UpdateEnabled: true, AvailableBytes: 1 << 30, ProtocolVersion: 1}
}

func (f *fixture) report(t *testing.T, assignment *protocol.Assignment) protocol.Observation {
	t.Helper()
	if _, err := f.db.DB.Exec("UPDATE nodes SET agent_version='2.0.0',last_seen=? WHERE id=?", f.now, assignment.NodeID); err != nil {
		t.Fatal(err)
	}
	return protocol.Observation{AssignmentID: assignment.ID, Nonce: assignment.Nonce, Version: "2.0.0", ReportAt: f.now, Outcome: "healthy"}
}

func fixtureSignaturePolicy() releasesignature.PolicyMetadata {
	policy := releasesignature.PolicyMetadata{Version: 1, Root: []byte(`{"fixture":"offline-root"}`), VerifierSHA256: strings.Repeat("e", 64), ReleaseWorkflow: "https://github.com/example/beat/.github/workflows/release.yml", OIDCIssuer: "https://token.actions.githubusercontent.com"}
	return sealFixturePolicy(policy)
}

func sealFixturePolicy(policy releasesignature.PolicyMetadata) releasesignature.PolicyMetadata {
	root := sha256.Sum256(policy.Root)
	policy.RootSHA256 = hex.EncodeToString(root[:])
	revision := sha256.Sum256([]byte(strings.Join([]string{"1", policy.RootSHA256, policy.VerifierSHA256, policy.CertificateIdentity, policy.ReleaseWorkflow, policy.OIDCIssuer}, "\x00")))
	policy.Revision = hex.EncodeToString(revision[:])
	return policy
}

func (v *fixtureVerifier) Policy() releasesignature.PolicyMetadata {
	if v.policy != nil {
		return *v.policy
	}
	return fixtureSignaturePolicy()
}
