package agentrollout

import (
	"testing"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestIgnoredWritesAndDeletedAuditCannotProduceSuccess(t *testing.T) {
	for _, table := range []string{"agent_release_artifacts", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			f.exec(t, "CREATE TRIGGER ignore_write BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(IGNORE);END")
			if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
				t.Fatal("ignored write reported success")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_releases", 0)
		})
	}
	f := newFixture(t)
	ids := f.nodes(t, 1)
	release := f.release(t)
	f.exec(t, "CREATE TRIGGER ignore_target BEFORE INSERT ON agent_rollout_targets BEGIN SELECT RAISE(IGNORE);END")
	if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "ignored", ReleaseID: release.ID, NodeIDs: ids}); err == nil {
		t.Fatal("missing targets reported success")
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollouts", 0)
	f.exec(t, "DROP TRIGGER ignore_target")
	f.exec(t, "CREATE TRIGGER delete_audit AFTER INSERT ON admin_audit_events BEGIN DELETE FROM admin_audit_events WHERE id=NEW.id;END")
	if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "unaudited", ReleaseID: release.ID, NodeIDs: ids}); err == nil {
		t.Fatal("deleted audit reported success")
	}
}

func TestAuthorityChangesAndDeferredCommitFailureRollBack(t *testing.T) {
	for _, kind := range []string{"owner", "commit"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			ids := f.nodes(t, 1)
			release := f.release(t)
			if kind == "owner" {
				f.exec(t, "CREATE TRIGGER revoke_owner AFTER INSERT ON admin_audit_events BEGIN UPDATE admin_users SET enabled=0;END")
			} else {
				f.exec(t, "CREATE TABLE commit_guard(id TEXT REFERENCES nodes(id) DEFERRABLE INITIALLY DEFERRED)")
				f.exec(t, "CREATE TRIGGER fail_commit AFTER INSERT ON agent_rollout_targets BEGIN INSERT INTO commit_guard VALUES('missing');END")
			}
			if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "rejected", ReleaseID: release.ID, NodeIDs: ids}); err == nil {
				t.Fatal("failed authority/commit admitted rollout")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollouts", 0)
		})
	}
	for _, kind := range []string{"credential", "commit"} {
		t.Run("agent/"+kind, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			if kind == "credential" {
				f.exec(t, "CREATE TRIGGER revoke_agent AFTER INSERT ON agent_rollout_events BEGIN UPDATE nodes SET agent_token_revoked_at=CURRENT_TIMESTAMP;END")
			} else {
				f.exec(t, "CREATE TABLE commit_guard(id TEXT REFERENCES nodes(id) DEFERRABLE INITIALLY DEFERRED)")
				f.exec(t, "CREATE TRIGGER fail_commit AFTER INSERT ON agent_rollout_events BEGIN INSERT INTO commit_guard VALUES('missing');END")
			}
			if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
				t.Fatal("failed agent admission leaked assignment", err)
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollout_attempts", 0)
		})
	}
}

func TestStoredReleaseTamperingBlocksAssignment(t *testing.T) {
	for _, query := range []string{"UPDATE agent_releases SET payload=X'01'", "UPDATE agent_releases SET signature=X'01'", "UPDATE agent_releases SET version='9.0.0'", "UPDATE agent_releases SET verified=0"} {
		t.Run(query, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			f.exec(t, query)
			if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || assignment != nil {
				t.Fatal("tampered release assigned", err)
			}
		})
	}
	f := newFixture(t)
	release := f.release(t)
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ReleaseNotesDigest = release.Manifest.Artifacts[0].SHA256
	f.sign(t, manifest)
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err != ErrConflict {
		t.Fatal("same version replaced immutable manifest", err)
	}
}

func TestDerivedReleaseTagCannotBeSuppliedByUpload(t *testing.T) {
	f := newFixture(t)
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = "2.1.0-rc.1+build.7"
	f.sign(t, manifest)
	if tag, err := ReleaseTag(manifest.Version); err != nil || tag != "v2.1.0-rc.1+build.7" {
		t.Fatal(tag, err)
	}
	if _, err := f.s.Import(t.Context(), f.actor, protocol.SignedManifest{Payload: f.signed.Payload, Signature: f.signed.Signature, SigstoreBundle: f.signed.SigstoreBundle}); err != nil {
		t.Fatal(err)
	}
}
