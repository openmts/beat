package agentrollout

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestEligibilityPolicyAndRemoteActivityBoundaries(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 1)
	release := f.release(t)
	policy, err := normalizePolicy(Policy{}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		reason string
		mutate func(*leaseCandidate, *protocol.Capabilities)
	}
	scenarios := []scenario{
		{"platform_changed", func(c *leaseCandidate, _ *protocol.Capabilities) { c.node.os = "freebsd" }},
		{"unknown_version", func(c *leaseCandidate, _ *protocol.Capabilities) { c.target.Version = "" }},
		{"downgrade_or_unknown_version", func(c *leaseCandidate, p *protocol.Capabilities) {
			c.target.Version = "3.0.0"
			c.node.version = "3.0.0"
			p.CurrentVersion = "3.0.0"
		}},
		{"stale_heartbeat", func(c *leaseCandidate, _ *protocol.Capabilities) { c.node.lastSeen = sql.NullTime{} }},
		{"platform_not_supported", func(c *leaseCandidate, _ *protocol.Capabilities) { c.release.Manifest.Artifacts[0].Support = "preview" }},
		{"maintenance_window", func(c *leaseCandidate, _ *protocol.Capabilities) { c.rollout.Policy.WindowStart = f.now.Add(time.Hour) }},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.reason, func(t *testing.T) {
			err := f.s.agentMutation(t.Context(), f.identities[ids[0]], func(tx *transaction, node nodeState) error {
				candidate := leaseCandidate{node: node, target: Target{NodeID: ids[0], Version: "1.0.0"}, release: release, rollout: Rollout{Policy: policy}}
				candidate.release.Manifest.Artifacts = append([]protocol.Artifact(nil), release.Manifest.Artifacts...)
				caps := capabilities()
				scenario.mutate(&candidate, &caps)
				_, reason, err := tx.eligible(candidate, caps)
				if err != nil || reason != scenario.reason {
					t.Fatal(reason, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := f.s.agentMutation(t.Context(), f.identities[ids[0]], func(tx *transaction, node nodeState) error {
		if err := tx.exec("CREATE TEMP TABLE operation_task_targets(node_id TEXT,state TEXT)"); err != nil {
			return err
		}
		if err := tx.exec("INSERT INTO operation_task_targets VALUES(?,'running')", ids[0]); err != nil {
			return err
		}
		candidate := leaseCandidate{node: node, target: Target{NodeID: ids[0], Version: "1.0.0"}, release: release, rollout: Rollout{Policy: policy}}
		_, reason, err := tx.eligible(candidate, capabilities())
		if err != nil || reason != "remote_operations_active" {
			t.Fatal(reason, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMissingCacheIsHeldAndVerifiedReimportRepairsCorruption(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.s.directory, manifest.Artifacts[0].SHA256+".artifact")
	if err := os.WriteFile(path, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || output != nil {
		t.Fatal(output, err)
	}
	_, targets, err = f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || targets[0].Reason != "artifact_missing" {
		t.Fatal(targets, err)
	}
	f.release(t)
	if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || assignment == nil {
		t.Fatal("verified repair did not unblock assignment", err)
	}
}

func TestCrossRolloutNodeConcurrencyNeverOffersTwoUpdates(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 1)
	release := f.release(t)
	for _, name := range []string{"first", "second"} {
		rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: name, ReleaseID: release.ID, NodeIDs: ids, Policy: Policy{MaxConcurrent: 10}})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
			t.Fatal(err)
		}
	}
	if assignment, err := f.s.Lease(t.Context(), f.identities[ids[0]], capabilities()); err != nil || assignment == nil {
		t.Fatal(err)
	}
	if assignment, err := f.s.Lease(t.Context(), f.identities[ids[0]], capabilities()); err != nil || assignment != nil {
		t.Fatal("second update offered concurrently", err)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_attempts", 1)
}

func TestPausedTimeoutStillRequiresExplicitUncertaintyAcknowledgment(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || assignment == nil {
		t.Fatal(err)
	}
	if err := f.s.Pause(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	f.expireAssignment(t)
	if err := f.s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID}); err != ErrConflict {
		t.Fatal("operator pause hid an uncertain timeout", err)
	}
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID, AcknowledgeUncertainty: true}); err != nil {
		t.Fatal(err)
	}
}
