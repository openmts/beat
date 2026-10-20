package service

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestEvaluationPreparedACKConflictAndDuplicateBatchRemainAtomic(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	f.report(t, 90, model.CollectedCustomSamples{})
	if _, err := f.evaluator.states.Admit(t.Context(), f.at); err != nil {
		t.Fatal(err)
	}
	key := store.AlertEvaluationKey{RuleID: f.rule.ID, NodeID: f.node.ID}
	input, policies, err := f.evaluator.prepare(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := alerteval.Step(input)
	if err != nil {
		t.Fatal(err)
	}
	change := store.AlertEvaluationCommit{Decision: decision, ExpectedStateVersion: input.Previous.Version, RuleRevision: input.Rule.Revision, Policies: policies}
	first, err := f.evaluator.notifications.Commit(t.Context(), change)
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	duplicate, err := f.evaluator.notifications.Commit(t.Context(), change)
	if err != nil || len(duplicate) != 1 || !duplicate[0].Duplicate || duplicate[0].MessageID != first[0].MessageID {
		t.Fatal(duplicate, err)
	}
	input, policies, err = f.evaluator.prepare(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	decision, err = alerteval.Step(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE alert_events SET status='acknowledged' WHERE id=?", input.Incident.ID); err != nil {
		t.Fatal(err)
	}
	change = store.AlertEvaluationCommit{Decision: decision, ExpectedStateVersion: input.Previous.Version, RuleRevision: input.Rule.Revision, Policies: policies}
	if _, err := f.evaluator.notifications.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("prepared progress overwrote concurrent ACK", err)
	}
}

func TestEvaluationRuntimePreservesDisabledAndRemovedScope(t *testing.T) {
	for _, statement := range []string{"UPDATE alert_rules SET enabled=0", "DELETE FROM nodes", "UPDATE alert_rules SET scope_kind='nodes'"} {
		t.Run(statement, func(t *testing.T) {
			f := newEvaluationRuntimeFixture(t)
			f.report(t, 90, model.CollectedCustomSamples{})
			f.run(t)
			previous := f.state(t)
			f.at = f.at.Add(alerteval.EvaluationInterval)
			if _, err := f.db.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			f.run(t)
			state := f.state(t)
			if state.ActiveEventID != previous.ActiveEventID || state.Status == model.EvaluationOK {
				t.Fatal(state)
			}
		})
	}
}

func TestEvaluationRunCancellationAndUnavailableEvidence(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	f.evaluator.Run(ctx, func(result alerteval.RoundResult, err error) {
		if err != nil || result.Completed != 1 {
			t.Error(result, err)
		}
		cancel()
	})
	if _, err := f.evaluator.RunOnce(ctx); err == nil {
		t.Fatal("cancelled round succeeded")
	}
	if _, err := NewAlertEvaluator(AlertEvaluatorConfig{Database: f.db.DB, Metrics: f.metrics, Queue: &evaluationBatchFake{}, Workers: alerteval.MaximumWorkers + 1}); err == nil {
		t.Fatal("invalid workers accepted")
	}
	if err := f.metrics.Close(); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	f.run(t)
	if state := f.state(t); state.Status != model.EvaluationError {
		t.Fatal(state)
	}
	f.evaluator.Run(ctx, nil)
}
