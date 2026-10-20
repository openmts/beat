package agentrollout

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestHealthGatesRejectBadCountersAndRequireContinuousFreshReports(t *testing.T) {
	for _, test := range []struct {
		input  protocol.Observation
		reason string
	}{{protocol.Observation{Outcome: "rolled_back"}, "agent_rolled_back"}, {protocol.Observation{Outcome: "healthy", Restarts: 2}, "restart_threshold"}, {protocol.Observation{Outcome: "healthy", ProtocolErrors: 1}, "protocol_error_threshold"}, {protocol.Observation{Outcome: "healthy", MetricErrors: 1}, "metric_error_threshold"}, {protocol.Observation{Outcome: "healthy", TaskFailures: 1}, "task_failure_threshold"}} {
		if reason := observationFailure(test.input, Policy{}); reason != test.reason {
			t.Fatal(reason)
		}
	}
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	// 只有Agent自报版本，没有成功写入Server的版本/报告证据，不会启动健康窗口。
	observation := protocol.Observation{AssignmentID: assignment.ID, Nonce: assignment.Nonce, Version: "2.0.0", ReportAt: f.now, Outcome: "healthy"}
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], observation); err != nil {
		t.Fatal(err)
	}
	_, state, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || state[0].Reason != "waiting_for_valid_report" {
		t.Fatal(state, err)
	}
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(6 * time.Minute)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal("healthy monitoring could not resume", err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal("gap counted as continuous healthy time", err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
}

func TestObservationRejectsImpossibleInputAndRegressingReports(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	good := f.report(t, assignment)
	for _, change := range []func(*protocol.Observation){func(o *protocol.Observation) { o.Restarts = -1 }, func(o *protocol.Observation) { o.MetricErrors = 1000001 }, func(o *protocol.Observation) { o.ReportAt = f.now.Add(time.Hour) }, func(o *protocol.Observation) { o.Outcome = "unknown" }} {
		input := good
		change(&input)
		if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], input); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], good); err != nil {
		t.Fatal(err)
	}
	old := good
	old.ReportAt = old.ReportAt.Add(-time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], old); err != ErrConflict {
		t.Fatal("regressing report accepted", err)
	}
}

func TestVersionTagPrefixDoesNotPreventHealthyWavePromotion(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	for _, advance := range []time.Duration{0, 31 * time.Second} {
		f.now = f.now.Add(advance)
		observation := f.report(t, assignment)
		observation.Version = "v2.0.0"
		if _, err := f.db.DB.Exec("UPDATE nodes SET agent_version='v2.0.0' WHERE id=?", assignment.NodeID); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], observation); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal("semantically identical version blocked health gate", err)
	}
}
