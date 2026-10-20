package agentrollout

import (
	"bytes"
	"testing"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestReleaseAndRolloutWritesFailClosed(t *testing.T) {
	for _, table := range []string{"agent_releases", "agent_release_artifacts"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			f.exec(t, "CREATE TRIGGER fail_write BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
				t.Fatal("failed release mutation accepted")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_releases", 0)
		})
	}
	for _, table := range []string{"agent_rollouts", "agent_rollout_targets", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			ids := f.nodes(t, 1)
			release := f.release(t)
			f.exec(t, "CREATE TRIGGER fail_write BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "failure", ReleaseID: release.ID, NodeIDs: ids}); err == nil {
				t.Fatal("failed rollout mutation accepted")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollouts", 0)
		})
	}
}

func TestAssignmentAndHealthWritesRemainAtomic(t *testing.T) {
	for _, table := range []string{"agent_rollout_attempts", "agent_rollout_events"} {
		t.Run("offer/"+table, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			f.exec(t, "CREATE TRIGGER fail_write BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || assignment != nil {
				t.Fatal("failed assignment leaked nonce", err)
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollout_attempts", 0)
		})
	}
	for _, table := range []string{"agent_rollout_targets", "agent_rollout_attempts", "agent_rollouts"} {
		t.Run("health/"+table, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
			if err != nil || assignment == nil {
				t.Fatal(err)
			}
			f.exec(t, "CREATE TRIGGER fail_write BEFORE UPDATE ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			input := protocol.Observation{AssignmentID: assignment.ID, Nonce: assignment.Nonce, Version: "1.0.0", ReportAt: f.now, Outcome: "failed"}
			if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], input); err == nil {
				t.Fatal("failed health mutation accepted")
			}
			f.count(t, "SELECT COUNT(*) FROM agent_rollout_targets WHERE state='offered'", 1)
		})
	}
}

func TestControlAndArtifactStateWriteErrors(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 1)
	f.exec(t, "CREATE TRIGGER fail_write BEFORE UPDATE ON agent_rollouts BEGIN SELECT RAISE(ABORT,'full');END")
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err == nil {
		t.Fatal("failed promotion accepted")
	}
	if err := f.s.Pause(t.Context(), f.actor, rollout.ID); err == nil {
		t.Fatal("failed pause accepted")
	}
	f.exec(t, "DROP TRIGGER fail_write")
	f.exec(t, "CREATE TRIGGER fail_event BEFORE INSERT ON agent_rollout_events BEGIN SELECT RAISE(ABORT,'full');END")
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err == nil {
		t.Fatal("promotion event failure accepted")
	}
	f.exec(t, "DROP TRIGGER fail_event")
	f.exec(t, "CREATE TRIGGER fail_artifact BEFORE UPDATE ON agent_release_artifacts BEGIN SELECT RAISE(ABORT,'full');END")
	if err := f.s.PutArtifact(t.Context(), f.actor, ArtifactUpload{ReleaseID: rollout.ReleaseID, ArtifactID: "linux-amd64", Body: bytes.NewReader(f.artifact)}); err == nil {
		t.Fatal("failed artifact state accepted")
	}
}

func (f *fixture) exec(t *testing.T, query string) {
	t.Helper()
	if _, err := f.db.DB.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) count(t *testing.T, query string, expected int) {
	t.Helper()
	var actual int
	if err := f.db.DB.QueryRow(query).Scan(&actual); err != nil || actual != expected {
		t.Fatal(query, expected, actual, err)
	}
}
