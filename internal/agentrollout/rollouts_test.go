package agentrollout

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestManualWavesRequireHealthObservationAndNeverAutoPromote(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 2)
	identity := f.identities[targets[0].NodeID]
	if assignment, err := f.s.Lease(t.Context(), identity, capabilities()); err != nil || assignment != nil {
		t.Fatal("created rollout dispatched automatically", err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), identity, capabilities())
	if err != nil || assignment == nil {
		t.Fatal(assignment, err)
	}
	if next, err := f.s.Lease(t.Context(), identity, capabilities()); err != nil || next != nil {
		t.Fatal("offered assignment replayed", err)
	}
	if next, err := f.s.Lease(t.Context(), f.identities[targets[1].NodeID], capabilities()); err != nil || next != nil {
		t.Fatal("future wave assigned", err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal("unhealthy wave promoted", err)
	}
	if err := f.s.Observe(t.Context(), identity, f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal("observation window skipped", err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.s.Observe(t.Context(), identity, f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.CurrentWave != 0 {
		t.Fatal("auto promotion occurred", err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.Lease(t.Context(), f.identities[targets[1].NodeID], capabilities())
	if err != nil || second == nil {
		t.Fatal(second, err)
	}
	failure := protocol.Observation{AssignmentID: second.ID, Nonce: second.Nonce, Version: "1.0.0", ReportAt: f.now, Outcome: "failed"}
	if err := f.s.Observe(t.Context(), f.identities[second.NodeID], failure); err != nil {
		t.Fatal(err)
	}
	current, _, err = f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.State != "paused" || current.Reason != "health_gate_failed" {
		t.Fatal(current, err)
	}
	var raw string
	if err := f.db.DB.QueryRow("SELECT observation_json FROM agent_rollout_attempts WHERE id=?", second.ID).Scan(&raw); err != nil || strings.Contains(raw, second.Nonce) {
		t.Fatal("assignment nonce stored as plaintext", err)
	}
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal(err)
	}
}

func TestPausedAssignmentsRemainReportableAndOwnerMayResume(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	if err := f.s.Pause(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal("pause discarded in-flight outcome", err)
	}
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.State != "completed" {
		t.Fatal(current, err)
	}
}
