package agentrollout

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestManifestRejectsTamperingAndRequiresBothSignatures(t *testing.T) {
	f := newFixture(t)
	for _, kind := range []string{"signature", "payload", "bundle", "verifier", "nil_verifier", "noncanonical", "duplicate_field", "unknown_field"} {
		t.Run(kind, func(t *testing.T) {
			signed := f.signed
			signed.Payload = bytes.Clone(signed.Payload)
			signed.Signature = bytes.Clone(signed.Signature)
			verifier := f.s.sigstore
			defer func() { f.s.sigstore = verifier; f.verifier.err = nil }()
			switch kind {
			case "signature":
				signed.Signature[0] ^= 1
			case "payload":
				signed.Payload = bytes.Replace(signed.Payload, []byte("2.0.0"), []byte("3.0.0"), 1)
			case "bundle":
				signed.SigstoreBundle = nil
			case "verifier":
				f.verifier.err = errors.New("untrusted evidence")
			case "nil_verifier":
				f.s.sigstore = nil
			case "noncanonical":
				signed.Payload = append(signed.Payload, '\n')
			case "duplicate_field":
				signed.Payload = append([]byte(`{"version":"3.0.0",`), signed.Payload[1:]...)
			case "unknown_field":
				signed.Payload = append([]byte(`{"unexpected":true,`), signed.Payload[1:]...)
			}
			if _, err := f.s.Import(t.Context(), f.actor, signed); err == nil {
				t.Fatal("untrusted release imported")
			}
		})
	}
	release, err := f.s.Import(t.Context(), f.actor, f.signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("revoked release resurrected")
	}
}

func TestManifestValidityAndTransitionProof(t *testing.T) {
	f := newFixture(t)
	original, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*protocol.Manifest){
		"expired": func(m *protocol.Manifest) { m.ExpiresAt = f.now }, "future": func(m *protocol.Manifest) { m.BuiltAt = f.now.Add(time.Hour) },
		"revoked": func(m *protocol.Manifest) { m.Revoked = true }, "schema": func(m *protocol.Manifest) { m.SchemaVersion = 9 },
		"protocol": func(m *protocol.Manifest) { m.MaximumProtocol = 0 }, "empty": func(m *protocol.Manifest) { m.Artifacts = nil },
		"duplicate": func(m *protocol.Manifest) { m.Artifacts = append(m.Artifacts, m.Artifacts[0]) },
		"path":      func(m *protocol.Manifest) { m.Artifacts[0].Filename = "../agent" }, "size": func(m *protocol.Manifest) { m.Artifacts[0].Size = MaximumArtifactBytes + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := original
			manifest.Artifacts = append([]protocol.Artifact(nil), original.Artifacts...)
			change(&manifest)
			if err := validateManifest(manifest, f.now); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	next := &protocol.KeyTransition{ID: "next", PublicKey: public, NotBefore: f.now, ExpiresAt: f.now.Add(time.Hour)}
	payload, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	next.Signature = ed25519.Sign(f.key, payload)
	if err := verifyTransition(f.key.Public().(ed25519.PublicKey), next, f.now); err != nil {
		t.Fatal(err)
	}
	next.PublicKey[0] ^= 1
	if err := verifyTransition(f.key.Public().(ed25519.PublicKey), next, f.now); err == nil {
		t.Fatal("forged key transition accepted")
	}
}

func TestArtifactsAreBoundedPrivateAndNeverAcceptedAfterAuditFailure(t *testing.T) {
	f := newFixture(t)
	release, err := f.s.Import(t.Context(), f.actor, f.signed)
	if err != nil {
		t.Fatal(err)
	}
	upload := ArtifactUpload{ReleaseID: release.ID, ArtifactID: "linux-amd64", Body: strings.NewReader("corrupt")}
	if err := f.s.PutArtifact(t.Context(), f.actor, upload); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	upload.Body = bytes.NewReader(append(bytes.Clone(f.artifact), 'x'))
	if err := f.s.PutArtifact(t.Context(), f.actor, upload); err == nil {
		t.Fatal("oversized artifact accepted")
	}
	if _, err := f.db.DB.Exec(`CREATE TRIGGER fail_artifact_audit BEFORE INSERT ON admin_audit_events WHEN NEW.action='release.artifact.import' BEGIN SELECT RAISE(ABORT,'full');END`); err != nil {
		t.Fatal(err)
	}
	upload.Body = bytes.NewReader(f.artifact)
	if err := f.s.PutArtifact(t.Context(), f.actor, upload); err == nil {
		t.Fatal("unaudited artifact marked ready")
	}
	var state string
	if err := f.db.DB.QueryRow("SELECT state FROM agent_release_artifacts").Scan(&state); err != nil || state != "missing" {
		t.Fatal(state, err)
	}
	if _, err := f.db.DB.Exec("DROP TRIGGER fail_artifact_audit"); err != nil {
		t.Fatal(err)
	}
	upload.Body = bytes.NewReader(f.artifact)
	if err := f.s.PutArtifact(t.Context(), f.actor, upload); err != nil {
		t.Fatal(err)
	}
	artifact := release.Manifest.Artifacts[0]
	path := filepath.Join(f.s.directory, artifact.SHA256+".artifact")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), path); err != nil {
		t.Fatal(err)
	}
	if err := f.s.checkArtifact(artifact); err == nil {
		t.Fatal("symlink release artifact accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	upload.Body = bytes.NewReader(f.artifact)
	if err := f.s.PutArtifact(ctx, f.actor, upload); err == nil {
		t.Fatal("canceled upload accepted")
	}
}

func TestOwnerRecentProofAndAuditFailureRemainAtomic(t *testing.T) {
	for _, query := range []string{"UPDATE admin_users SET role='admin'", "UPDATE admin_users SET enabled=0", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP", "UPDATE admin_sessions SET reauthenticated_until=NULL"} {
		t.Run(query, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.db.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
				t.Fatal("ineligible owner imported release")
			}
		})
	}
	f := newFixture(t)
	if _, err := f.db.DB.Exec(`CREATE TRIGGER fail_release_audit BEFORE INSERT ON admin_audit_events WHEN NEW.action='release.import' BEGIN SELECT RAISE(ABORT,'full');END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("unaudited release imported")
	}
	var count int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM agent_releases").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
