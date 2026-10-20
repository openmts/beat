package agentrollout

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestThresholdFailureToleratesOnlyBudgetedTransientErrors(t *testing.T) {
	policy := Policy{MaxRestarts: 0, MaxProtocolErrors: 0, MaxMetricErrors: 0, MaxTaskFailures: 0}
	transient := protocol.Observation{Outcome: "healthy", Restarts: 1, ProtocolErrors: 1, MetricErrors: 1, TaskFailures: 1}
	for _, test := range []struct {
		name, strict, budgeted string
		input                  protocol.Observation
	}{
		{name: "transient counters", strict: "protocol_error_threshold", budgeted: "", input: transient},
		{name: "second restart absorbed", strict: "restart_threshold", budgeted: "", input: protocol.Observation{Outcome: "healthy", Restarts: 2}},
		{name: "restarts past budget", strict: "restart_threshold", budgeted: "restart_threshold", input: protocol.Observation{Outcome: "healthy", Restarts: 3}},
		{name: "metric errors past budget", strict: "metric_error_threshold", budgeted: "metric_error_threshold", input: protocol.Observation{Outcome: "healthy", MetricErrors: 2}},
		{name: "protocol errors past budget", strict: "protocol_error_threshold", budgeted: "protocol_error_threshold", input: protocol.Observation{Outcome: "healthy", ProtocolErrors: 2}},
		{name: "task failures past budget", strict: "task_failure_threshold", budgeted: "task_failure_threshold", input: protocol.Observation{Outcome: "healthy", TaskFailures: 2}},
		{name: "explicit failure ignores budget", strict: "agent_failed", budgeted: "agent_failed", input: protocol.Observation{Outcome: "failed"}},
		{name: "explicit rollback ignores budget", strict: "agent_rolled_back", budgeted: "agent_rolled_back", input: protocol.Observation{Outcome: "rolled_back"}},
	} {
		if reason := thresholdFailure(test.input, policy, 0); reason != test.strict {
			t.Fatalf("%s: strict gate returned %q want %q", test.name, reason, test.strict)
		}
		if reason := thresholdFailure(test.input, policy, toleratedTransientErrors); reason != test.budgeted {
			t.Fatalf("%s: budgeted gate returned %q want %q", test.name, reason, test.budgeted)
		}
	}
	if budget := transientErrorBudget("observing"); budget != 0 {
		t.Fatalf("observing target got a transient budget: %d", budget)
	}
	if budget := transientErrorBudget("healthy"); budget != toleratedTransientErrors {
		t.Fatalf("healthy target lost its transient budget: %d", budget)
	}
}

func TestHealthyCanaryToleratesOneTransientErrorBudget(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if state := f.targetState(t, rollout.ID); state.State != "healthy" {
		t.Fatalf("canary never became healthy: %+v", state)
	}
	// 一次瞬时限流（例如 /nodes/report 429 让 Agent 记下一次 metric_error）不再推翻金丝雀。
	transient := f.report(t, assignment)
	transient.MetricErrors = 1
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], transient); err != nil {
		t.Fatal(err)
	}
	state := f.targetState(t, rollout.ID)
	current, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "healthy" || state.Reason != "" || current.State != "active" {
		t.Fatalf("transient error flipped a healthy canary: %+v %+v", state, current)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_events WHERE kind='assignment.healthy' AND reason='transient_error_tolerated'", 1)
	// Agent 的计数是累计值：healthy 之后的每个上报周期都还会命中同一个越界，
	// 事件必须判重，否则一次瞬时限流会在每个周期各写一条同名事件。
	repeated := f.report(t, assignment)
	repeated.MetricErrors = 1
	f.now = f.now.Add(5 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], repeated); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(5 * time.Second)
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], f.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if state := f.targetState(t, rollout.ID); state.State != "healthy" {
		t.Fatalf("repeated transient report flipped a healthy canary: %+v", state)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_events WHERE kind='assignment.healthy' AND reason='transient_error_tolerated'", 1)
	// 预算只覆盖一次：第二次越界仍然是真实失败。
	persistent := f.report(t, assignment)
	persistent.MetricErrors = 2
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], persistent); err != nil {
		t.Fatal(err)
	}
	state = f.targetState(t, rollout.ID)
	current, _, err = f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "failed" || state.Reason != "metric_error_threshold" || current.State != "paused" || current.Reason != "health_gate_failed" {
		t.Fatalf("second breach did not fail the canary: %+v %+v", state, current)
	}
}

func TestObservationWindowStaysStrictAndExplicitFailureBeatsHealthy(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	breach := f.report(t, assignment)
	breach.MetricErrors = 1
	if err := f.s.Observe(t.Context(), f.identities[assignment.NodeID], breach); err != nil {
		t.Fatal(err)
	}
	if state := f.targetState(t, rollout.ID); state.State != "failed" || state.Reason != "metric_error_threshold" {
		t.Fatalf("observation window lost its strict gate: %+v", state)
	}

	second := newFixture(t)
	rollout, targets = second.rollout(t, 1)
	if err := second.s.Promote(t.Context(), second.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err = second.s.Lease(t.Context(), second.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	if err := second.s.Observe(t.Context(), second.identities[assignment.NodeID], second.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	second.now = second.now.Add(31 * time.Second)
	if err := second.s.Observe(t.Context(), second.identities[assignment.NodeID], second.report(t, assignment)); err != nil {
		t.Fatal(err)
	}
	if state := second.targetState(t, rollout.ID); state.State != "healthy" {
		t.Fatalf("second fixture never became healthy: %+v", state)
	}
	failing := second.report(t, assignment)
	failing.Outcome = "failed"
	if err := second.s.Observe(t.Context(), second.identities[assignment.NodeID], failing); err != nil {
		t.Fatal(err)
	}
	if state := second.targetState(t, rollout.ID); state.State != "failed" || state.Reason != "agent_failed" {
		t.Fatalf("explicit Agent failure was swallowed by the budget: %+v", state)
	}
}

func (f *fixture) targetState(t *testing.T, rolloutID string) Target {
	t.Helper()
	_, targets, err := f.s.Detail(t.Context(), f.actor, rolloutID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("read rollout targets: %v %+v", err, targets)
	}
	return targets[0]
}
