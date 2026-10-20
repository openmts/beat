package service

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestEvaluationRuntimePersistsTriggerDuplicateAndManualOnlyACK(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	f.report(t, 90, model.CollectedCustomSamples{})
	f.run(t)
	state := f.state(t)
	if state.ActiveEventID == "" || state.Phase != model.AlertPhaseFiring {
		t.Fatal(state)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	f.run(t)
	repeated := f.state(t)
	if repeated.ActiveEventID != state.ActiveEventID || repeated.EvidenceID != state.EvidenceID {
		t.Fatal("duplicate receipt changed source", repeated)
	}
	if _, err := f.db.DB.Exec("UPDATE alert_events SET status='acknowledged' WHERE id=?", state.ActiveEventID); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	f.report(t, 10, model.CollectedCustomSamples{})
	f.run(t)
	acknowledged := f.state(t)
	if acknowledged.ActiveEventStatus != model.AlertStatusAcknowledged || acknowledged.ActiveEventID != state.ActiveEventID {
		t.Fatal("ACK auto-resolved", acknowledged)
	}
	var messages int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages").Scan(&messages); err != nil || messages != 1 {
		t.Fatal(messages, err)
	}
}

func TestEvaluationRuntimeHealthRecoveryAndTriggerBatchRollback(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	if _, err := f.db.DB.Exec("UPDATE alert_rules SET no_data_policy='notify',no_data_delay_seconds=0"); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	unhealthy := f.state(t)
	if unhealthy.Health.EpochID == "" || unhealthy.Health.NotifiedMask == 0 {
		t.Fatal(unhealthy)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	f.report(t, 90, model.CollectedCustomSamples{})
	if _, err := f.db.DB.Exec(`CREATE TRIGGER reject_second BEFORE INSERT ON notification_messages WHEN NEW.event_kind='alert.triggered' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := f.evaluator.RunOnce(t.Context())
	if err != nil || result.Failed != 1 {
		t.Fatal(result, err)
	}
	failed := f.state(t)
	if failed.Version != unhealthy.Version || failed.Health.EpochID != unhealthy.Health.EpochID || failed.ActiveEventID != "" {
		t.Fatal("partial batch commit", failed)
	}
	if _, err := f.db.DB.Exec("DROP TRIGGER reject_second"); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	recovered := f.state(t)
	if recovered.Health.EpochID != "" || recovered.ActiveEventID == "" {
		t.Fatal(recovered)
	}
	var messages int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages").Scan(&messages); err != nil || messages != 3 {
		t.Fatal(messages, err)
	}
}

func TestEvaluationRuntimeReleasesConflictingLeaseAndHandlesDisabled(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	if _, err := NewAlertEvaluator(AlertEvaluatorConfig{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	if _, err := f.evaluator.states.Admit(t.Context(), f.at); err != nil {
		t.Fatal(err)
	}
	pairs, err := f.evaluator.Due(t.Context(), alerteval.DueQuery{At: f.at, Limit: 1})
	if err != nil || len(pairs) != 1 {
		t.Fatal(pairs, err)
	}
	f.evaluator.running.Store(true)
	if _, err := f.evaluator.RunOnce(t.Context()); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	f.evaluator.running.Store(false)
	if _, err := f.db.DB.Exec("UPDATE metric_definitions SET enabled=0 WHERE metric_key='cpu'"); err != nil {
		t.Fatal(err)
	}
	if err := f.evaluator.Evaluate(t.Context(), pairs[0]); err != nil {
		t.Fatal(err)
	}
	state := f.state(t)
	if state.Status != model.EvaluationDisabled {
		t.Fatal(state)
	}
	if err := f.evaluator.Evaluate(t.Context(), pairs[0]); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	_, err = f.evaluator.states.Get(t.Context(), store.AlertEvaluationKey{RuleID: f.rule.ID, NodeID: f.node.ID})
	if err != nil {
		t.Fatal(err)
	}
}
