package agentrollout

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestClosedStorageAndCorruptPolicyFailClosed(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 1)
	f.exec(t, "UPDATE agent_rollouts SET policy_json='{'")
	if _, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID); err == nil {
		t.Fatal("corrupt policy accepted")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("closed store imported")
	}
	if _, err := f.s.Lease(t.Context(), model.AgentIdentity{}, capabilities()); err == nil {
		t.Fatal("closed store assigned")
	}
	if err := f.s.Recover(t.Context()); err == nil {
		t.Fatal("closed store recovered")
	}
	if err := f.s.initializeTrust(t.Context()); err == nil {
		t.Fatal("closed trust store accepted")
	}
}

func TestRecoveryMutationFailuresPreserveOriginalState(t *testing.T) {
	for _, table := range []string{"agent_rollout_attempts", "agent_rollout_targets", "agent_rollouts"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || assignment == nil {
				t.Fatal(err)
			}
			f.exec(t, "CREATE TRIGGER reject_recovery BEFORE UPDATE ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			f.expireAssignment(t)
			if err := f.s.Recover(t.Context()); err == nil {
				t.Fatal("failed recovery reported success")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollout_targets WHERE state='offered'", 1)
		})
	}
}

func TestMissingAuthorityAndMetadataTablesRejectWork(t *testing.T) {
	for _, table := range []string{"admin_users", "agent_releases", "agent_rollout_targets"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			f.exec(t, "ALTER TABLE "+table+" RENAME TO unavailable_table")
			if table == "admin_users" {
				if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
					t.Fatal("missing owner table accepted")
				}
			} else {
				if _, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil {
					t.Fatal("missing assignment table accepted")
				}
			}
		})
	}
}

func TestPrivateCacheParentsAndHashFailures(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	artifact := release.Manifest.Artifacts[0]
	path := filepath.Join(f.s.directory, artifact.SHA256+".artifact")
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, int(artifact.Size)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.s.checkArtifact(artifact); err == nil {
		t.Fatal("same-size digest corruption accepted")
	}
	if err := f.s.publishArtifact(filepath.Join(t.TempDir(), "missing"), artifact); err == nil {
		t.Fatal("missing staged file published")
	}
	parent := t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink(f.s.directory, link); err != nil {
		t.Fatal(err)
	}
	if err := privateReleaseDirectory(filepath.Join(link, "child")); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if err := os.Symlink("loop", filepath.Join(parent, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := secureReleaseParents(filepath.Join(parent, "loop", "child", "leaf")); err == nil {
		t.Fatal("looping ancestor accepted")
	}
	invalid := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(invalid, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.s.directory = invalid
	if _, err := f.s.stageArtifact(context.Background(), artifact, bytes.NewReader(f.artifact)); err == nil {
		t.Fatal("invalid cache accepted")
	}
}
