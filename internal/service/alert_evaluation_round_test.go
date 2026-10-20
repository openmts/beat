package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func TestEvaluationRoundExposesDeferredLagAndClockRollback(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	if _, err := f.db.DB.Exec("UPDATE alert_rules SET no_data_policy='notify',no_data_delay_seconds=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.evaluator.states.Admit(t.Context(), f.at); err != nil {
		t.Fatal(err)
	}
	f.evaluator.notifications.queue = &evaluationBatchFake{err: notificationqueue.ErrQueueCapacity}
	f.at = f.at.Add(3 * alerteval.EvaluationInterval)
	result, err := f.evaluator.RunOnce(t.Context())
	if err != nil || result.Deferred != 1 || !result.BacklogComplete || !result.Degraded || result.OldestDueAge < alerteval.MaximumPollGap {
		t.Fatal(result, err)
	}
}

func TestEvaluationRuntimeClockRollbackNeverCreditsPendingTime(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	if _, err := f.db.DB.Exec("UPDATE alert_rules SET duration=60"); err != nil {
		t.Fatal(err)
	}
	f.report(t, 90, model.CollectedCustomSamples{})
	f.run(t)
	previous := f.state(t)
	f.at = f.at.Add(-5 * time.Minute)
	result, err := f.evaluator.RunOnce(t.Context())
	if err != nil || result.Failed != 1 || result.Deferred != 1 {
		t.Fatal(result, err)
	}
	state := f.state(t)
	if state.Version != previous.Version+1 || state.TriggerElapsedMS != 0 || state.RecoveryElapsedMS != 0 || state.Reason != "clock_rollback" || state.EvidenceID != previous.EvidenceID || state.ActiveEventID != "" {
		t.Fatal(state)
	}
}

type cancellingEvaluationSource struct {
	cancel context.CancelFunc
	called bool
	at     time.Time
}

func (s *cancellingEvaluationSource) Due(context.Context, alerteval.DueQuery) ([]alerteval.DuePair, error) {
	if s.called {
		return nil, nil
	}
	s.called = true
	return []alerteval.DuePair{{RuleID: "rule", NodeID: "node", DueAt: s.at}}, nil
}

func (s *cancellingEvaluationSource) Evaluate(context.Context, alerteval.DuePair) error {
	s.cancel()
	return context.Canceled
}

func TestEvaluationRoundCancellationDoesNotInventCompleteBacklog(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheduler, err := alerteval.NewScheduler(&cancellingEvaluationSource{cancel: cancel, at: f.at}, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.evaluator.scheduler = scheduler
	result, err := f.evaluator.evaluateRound(ctx, f.at)
	if !errors.Is(err, context.Canceled) || result.BacklogComplete {
		t.Fatal(result, err)
	}
	if _, err := f.evaluator.evaluateRound(ctx, f.at); err == nil {
		t.Fatal("cancelled backlog query hidden")
	}
}
