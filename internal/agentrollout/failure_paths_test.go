package agentrollout

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasesignature"
)

type verificationHook func(context.Context, releasesignature.Evidence) error

func (hook verificationHook) Verify(ctx context.Context, evidence releasesignature.Evidence) error {
	return hook(ctx, evidence)
}

func TestImportRechecksTrustRevisionAndActorAfterVerification(t *testing.T) {
	for _, query := range []string{"UPDATE agent_release_trust SET revision=revision+1", "UPDATE agent_release_trust SET revoked_at=CURRENT_TIMESTAMP", "UPDATE admin_users SET enabled=0"} {
		t.Run(query, func(t *testing.T) {
			f := newFixture(t)
			f.s.sigstore = verificationHook(func(context.Context, releasesignature.Evidence) error { _, err := f.db.DB.Exec(query); return err })
			if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
				t.Fatal("old verification snapshot accepted")
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM agent_releases").Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestKeyRevocationStopsNewAssignments(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeKey(t.Context(), f.actor, "root"); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.State != "paused" {
		t.Fatal(current, err)
	}
	if result, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || result != nil {
		t.Fatal(result, err)
	}
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID, AcknowledgeUncertainty: true}); err == nil {
		t.Fatal("revoked trust resumed")
	}
	if err := f.s.RevokeKey(t.Context(), f.actor, "missing"); err == nil {
		t.Fatal("missing trust revoked")
	}
}

func TestNewValidatesPinnedTrustAndPrivateDirectory(t *testing.T) {
	f := newFixture(t)
	key := f.s.keys["root"]
	base := Config{Database: f.db.DB, Directory: filepath.Join(t.TempDir(), "cache"), Keys: []TrustedKey{key}, Sigstore: f.verifier, Enabled: true, ServerSchema: 14, Now: func() time.Time { return f.now }}
	for _, change := range []func(*Config){func(c *Config) { c.Database = nil }, func(c *Config) { c.Keys = nil }, func(c *Config) { c.Sigstore = nil }, func(c *Config) { c.Keys = []TrustedKey{key, key} }, func(c *Config) { c.Directory = "relative" }, func(c *Config) { c.Keys[0].PublicKey = nil }, func(c *Config) { c.ServerSchema = 0 }} {
		config := base
		config.Keys = append([]TrustedKey(nil), base.Keys...)
		change(&config)
		if _, err := New(config); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Keys = []TrustedKey{key}
	changed.Keys[0].PublicKey = public
	if _, err := New(changed); err == nil {
		t.Fatal("pinned key changed under same ID")
	}
	if _, err := New(base); err != nil {
		t.Fatal("unchanged trust failed restart", err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := privateReleaseDirectory(blocked); err == nil {
		t.Fatal("file accepted as cache")
	}
	unsafe := t.TempDir()
	if err := os.Chmod(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := privateReleaseDirectory(unsafe); err == nil {
		t.Fatal("public cache accepted")
	}
}

func TestInvalidPoliciesAndArtifactInputsFailBeforeMutation(t *testing.T) {
	f := newFixture(t)
	for _, policy := range []Policy{{CanaryCount: -1}, {WaveSize: -1}, {MaxConcurrent: 101}, {ObservationSeconds: 1}, {ReportFreshnessSeconds: 1}, {MaxRestarts: -1}, {WindowStart: f.now, WindowEnd: f.now.Add(-time.Second)}} {
		if _, err := normalizePolicy(policy, f.now); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	if _, err := f.s.Create(t.Context(), f.actor, CreateInput{}); err == nil {
		t.Fatal("empty rollout accepted")
	}
	if _, err := decodeManifest(nil); err == nil {
		t.Fatal("empty manifest accepted")
	}
	if _, err := decodeManifest([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing manifest data accepted")
	}
	if _, err := f.s.Import(t.Context(), f.actor, protocol.SignedManifest{}); err == nil {
		t.Fatal("empty signature accepted")
	}
	release := f.release(t)
	if err := f.s.PutArtifact(t.Context(), f.actor, ArtifactUpload{ReleaseID: release.ID, ArtifactID: "unknown", Body: bytes.NewReader(nil)}); err == nil {
		t.Fatal("unknown artifact accepted")
	}
	if err := f.s.PutArtifact(t.Context(), f.actor, ArtifactUpload{ReleaseID: release.ID, ArtifactID: "linux-amd64"}); err == nil {
		t.Fatal("nil artifact input accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (contextReader{ctx: ctx, reader: bytes.NewReader(nil)}).Read(make([]byte, 1)); err == nil {
		t.Fatal("canceled stream read")
	}
	if file, err := openArtifactFile(t.TempDir()); err == nil {
		_ = file.Close()
		t.Fatal("directory opened as artifact")
	}
	if _, err := openArtifactFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing artifact opened")
	}
}

func TestDeniedAndMissingControlAndObservation(t *testing.T) {
	f := newFixture(t)
	for _, action := range []func() error{func() error { return f.s.Pause(t.Context(), f.actor, "missing") }, func() error { return f.s.Abort(t.Context(), f.actor, "missing") }, func() error { return f.s.Resume(t.Context(), f.actor, ResumeInput{ID: "missing"}) }, func() error { return f.s.Promote(t.Context(), f.actor, "missing") }, func() error { return f.s.RevokeRelease(t.Context(), f.actor, "missing") }} {
		if err := action(); err == nil {
			t.Fatal("missing resource control succeeded")
		}
	}
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID}); err != ErrConflict {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	observation := f.report(t, assignment)
	observation.Nonce = "invalid"
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], observation); err == nil {
		t.Fatal("malformed nonce accepted")
	}
	observation.Nonce = string(bytes.Repeat([]byte{'f'}, 64))
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], observation); err == nil {
		t.Fatal("forged nonce accepted")
	}
	observation = f.report(t, assignment)
	observation.AssignmentID = "missing"
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], observation); err == nil {
		t.Fatal("wrong assignment accepted")
	}
	identity := f.identities[assignment.NodeID]
	identity.CredentialFingerprint = "revoked"
	if _, err := f.s.Lease(t.Context(), identity, capabilities()); err == nil {
		t.Fatal("revoked agent received assignment")
	}
}

func TestHelpersBoundValuesAndRequirePersistedRows(t *testing.T) {
	if bounded("abcd", 2) != "ab" {
		t.Fatal("unbounded audit field")
	}
	if err := requireOne(nil, errors.New("failure")); err == nil {
		t.Fatal("SQL error ignored")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("CREATE TABLE empty(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("UPDATE empty SET id='value'")
	if err := requireOne(result, err); err == nil {
		t.Fatal("zero-row write accepted")
	}
}

func (hook verificationHook) Policy() releasesignature.PolicyMetadata {
	return fixtureSignaturePolicy()
}
