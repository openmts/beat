package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestAuthorizeEvaluationCommitHandlesRetiredRulesAndOutOfScopeStates(t *testing.T) {
	sqlite, _, change := evaluationCommitFixture(t)
	if _, err := sqlite.DB.Exec("UPDATE alert_rules SET enabled=0"); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	retired := change
	retired.Decision.State.Status = model.EvaluationDisabled
	retired.Decision.Actions = nil
	if err := authorizeEvaluationCommit(t.Context(), transaction, retired); err != nil {
		t.Fatalf("retired rule with disabled decision: %v", err)
	}
	active := retired
	active.Decision.State.Status = model.EvaluationOK
	if err := authorizeEvaluationCommit(t.Context(), transaction, active); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("retired rule with active decision = %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE alert_rules SET enabled=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("DELETE FROM nodes"); err != nil {
		t.Fatal(err)
	}
	transaction = alertTestTx(t, sqlite.DB)
	outOfScope := change
	outOfScope.Decision.State.Status = model.EvaluationOutOfScope
	outOfScope.Decision.Actions = nil
	if err := authorizeEvaluationCommit(t.Context(), transaction, outOfScope); err != nil {
		t.Fatalf("out-of-scope decision: %v", err)
	}
	stale := outOfScope
	stale.Decision.State.Status = model.EvaluationOK
	if err := authorizeEvaluationCommit(t.Context(), transaction, stale); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("out-of-scope rule with active decision = %v", err)
	}
}

func TestAuthorizeEvaluationCommitAcceptsFreshPolicyEvidence(t *testing.T) {
	sqlite, _, change := evaluationCommitFixture(t)
	transaction := alertTestTx(t, sqlite.DB)
	if err := authorizeEvaluationCommit(t.Context(), transaction, change); err != nil {
		t.Fatalf("fresh evaluation authority = %v", err)
	}
	withoutEvidence := change
	withoutEvidence.Policies = nil
	if err := authorizeEvaluationCommit(t.Context(), transaction, withoutEvidence); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("scalar rule without policy evidence = %v", err)
	}
}

func TestRecheckEvaluationMetricPoliciesRejectsMissingDisabledAndStaleEvidence(t *testing.T) {
	sqlite, _, change := evaluationCommitFixture(t)
	if _, err := sqlite.DB.Exec("UPDATE metric_definitions SET enabled=0 WHERE metric_key='cpu'"); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	missing := change
	missing.Policies = covEvaluationRefs("metric-outside-catalog")
	if err := recheckEvaluationMetricPolicies(t.Context(), transaction, missing); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("unknown metric policy = %v", err)
	}
	stale := change
	stale.Policies = covEvaluationRefs("cpu")
	stale.Policies[0].Revision = covStaleRevision
	if err := recheckEvaluationMetricPolicies(t.Context(), transaction, stale); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("stale metric revision = %v", err)
	}
	disabled := change
	disabled.Policies = covEvaluationRefs("cpu")
	disabled.Decision.State.Status = model.EvaluationDisabled
	disabled.Decision.Actions = nil
	if err := recheckEvaluationMetricPolicies(t.Context(), transaction, disabled); err != nil {
		t.Fatalf("disabled metric with disabled decision: %v", err)
	}
	active := disabled
	active.Decision.State.Status = model.EvaluationOK
	if err := recheckEvaluationMetricPolicies(t.Context(), transaction, active); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("disabled metric with active decision = %v", err)
	}
}

func TestAuthorizeEvaluationCommitRechecksAcceleratorEvidence(t *testing.T) {
	sqlite, _, change := evaluationCommitFixture(t)
	if _, err := sqlite.DB.Exec("UPDATE alert_rules SET metric=?, device_id=?", model.MetricGPUMemoryPercent, model.AcceleratorAnyDevice); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	change.Decision.State.DeviceID = covAcceleratorDevice
	change.Policies = covEvaluationRefs("gpu_memory_used_bytes", "gpu_memory_total_bytes")
	if err := authorizeEvaluationCommit(t.Context(), transaction, change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("accelerator evidence without snapshot = %v", err)
	}
}

func TestRecheckCustomExecutionPolicySurfacesStorageFailures(t *testing.T) {
	closed := covClosedHandle(t)
	reference := customExecutionReference{nodeID: "custom-node", reference: AlertMetricPolicyReference{Key: model.CustomMetricPrefix + "temp_probe"}}
	if err := recheckCustomExecutionPolicy(t.Context(), closed, reference); err == nil {
		t.Fatal("custom execution recheck ignored a closed database")
	}
}
