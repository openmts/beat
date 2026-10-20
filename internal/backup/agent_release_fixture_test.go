package backup

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

// seedArchivedAgentRelease要求调用方先在自己的临时库执行MigrateAgentRollout。
func seedArchivedAgentRelease(t *testing.T, database *sql.DB) model.BackupAgentReleaseArtifact {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{43}, ed25519.SeedSize))
	now := time.Now().UTC()
	artifactData := []byte("excluded-binary-cache-material")
	digest := sha256.Sum256(artifactData)
	artifact := protocol.Artifact{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: int64(len(artifactData)), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "2.0.0", Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "archive-root", Artifacts: []protocol.Artifact{artifact}}
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := protocol.ManifestDigest(payload)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_release_trust(key_id,public_key,not_before,expires_at,created_at) VALUES(?,?,?,?,?)`, []any{"archive-root", key.Public().(ed25519.PublicKey), now.Add(-time.Hour), now.Add(2 * time.Hour), now}},
		{`INSERT INTO agent_release_signature_policies(revision,root_sha256,policy_json,created_at) VALUES(?,?,?,?)`, []any{"archive-policy", strings.Repeat("e", 64), []byte(`{"root_path":"/must-not-read","issuer":"https://must-not-fetch.invalid"}`), now}},
		{`INSERT INTO agent_release_signature_state(id,policy_revision,updated_at) VALUES(1,?,?)`, []any{"archive-policy", now}},
		{`INSERT INTO agent_releases(id,version,channel,signing_key_id,payload,signature,sigstore_bundle,expires_at,trust_revision,signature_policy_revision,verified,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,1,?,1,?,?)`, []any{releaseID, manifest.Version, manifest.Channel, manifest.SigningKeyID, payload, ed25519.Sign(key, payload), []byte("opaque archived Sigstore evidence"), manifest.ExpiresAt, "archive-policy", "archive-owner", now}},
		{`INSERT INTO agent_release_artifacts(release_id,artifact_id,digest,size,state,updated_at) VALUES(?,?,?,?,'ready',?)`, []any{releaseID, artifact.ID, artifact.SHA256, artifact.Size, now}},
	}
	for _, statement := range statements {
		if _, err = database.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	return model.BackupAgentReleaseArtifact{ReleaseID: releaseID, ArtifactID: artifact.ID, SHA256: artifact.SHA256, Size: artifact.Size}
}
