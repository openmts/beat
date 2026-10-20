package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type evaluationCommitFake struct {
	direct, transaction int
	err                 error
}

func (f *evaluationCommitFake) Commit(context.Context, store.AlertEvaluationCommit) error {
	f.direct++
	return f.err
}

func (f *evaluationCommitFake) CommitTx(context.Context, *sql.Tx, store.AlertEvaluationCommit) error {
	f.transaction++
	return f.err
}

type evaluationBatchFake struct {
	requests []notificationqueue.Request
	err      error
}

func (f *evaluationBatchFake) EnqueueBatch(_ context.Context, requests []notificationqueue.Request, mutation func(*sql.Tx) error) ([]notificationqueue.Receipt, error) {
	f.requests = requests
	if f.err != nil {
		return nil, f.err
	}
	if err := mutation(nil); err != nil {
		return nil, err
	}
	return make([]notificationqueue.Receipt, len(requests)), nil
}

func TestEvaluationNotificationCommitsProgressOrAtomicBatch(t *testing.T) {
	states, queue := &evaluationCommitFake{}, &evaluationBatchFake{}
	service := NewAlertEvaluationNotificationService(states, queue)
	if _, err := service.Commit(t.Context(), store.AlertEvaluationCommit{}); err != nil || states.direct != 1 {
		t.Fatal(err)
	}
	event := &model.AlertEvaluationEvent{ID: "health", EpochID: "epoch", SourceKey: "source", Kind: "recovered", OccurredAt: time.Now()}
	change := store.AlertEvaluationCommit{Decision: alerteval.StepResult{Actions: []alerteval.Action{
		{Kind: alerteval.ActionHealth, Notify: true, Health: event},
		{Kind: alerteval.ActionThresholdTrigger, Notify: true, Event: &model.AlertEvent{ID: "incident", Status: model.AlertStatusTriggered}},
	}}}
	receipts, err := service.Commit(t.Context(), change)
	if err != nil || len(receipts) != 2 || states.transaction != 1 || states.direct != 1 {
		t.Fatal(receipts, err)
	}
	if _, ok := queue.requests[0].Data["metric.value"]; ok {
		t.Fatal("health invented numeric evidence")
	}
	if queue.requests[0].Data["evaluation.epoch"] != "epoch" || queue.requests[0].EventKind != "alert.evaluation_recovered" {
		t.Fatal(queue.requests)
	}
	states.err = alerteval.ErrConflict
	if _, err := service.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	queue.err = notificationqueue.ErrQueueCapacity
	if _, err := service.Commit(t.Context(), change); !errors.Is(err, notificationqueue.ErrQueueCapacity) {
		t.Fatal(err)
	}
}

func TestEvaluationNotificationValidation(t *testing.T) {
	states := &evaluationCommitFake{}
	service := NewAlertEvaluationNotificationService(nil, nil)
	if _, err := service.Commit(t.Context(), store.AlertEvaluationCommit{}); !errors.Is(err, alerteval.ErrUnavailable) {
		t.Fatal(err)
	}
	service.states = states
	valid := alerteval.Action{Kind: alerteval.ActionHealth, Notify: true, Health: &model.AlertEvaluationEvent{Kind: "error"}}
	if _, err := service.Commit(t.Context(), store.AlertEvaluationCommit{Decision: alerteval.StepResult{Actions: []alerteval.Action{valid}}}); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
		t.Fatal(err)
	}
	invalid := [][]alerteval.Action{{valid, valid, valid}, {{Kind: "unknown", Notify: true}}, {{Kind: alerteval.ActionHealth, Notify: true}}, {{Kind: alerteval.ActionThresholdRecovery, Notify: true}}, {{Kind: alerteval.ActionHealth, Notify: true, Health: &model.AlertEvaluationEvent{Kind: "unknown"}}}}
	for _, actions := range invalid {
		if _, err := service.Commit(t.Context(), store.AlertEvaluationCommit{Decision: alerteval.StepResult{Actions: actions}}); !errors.Is(err, alerteval.ErrConflict) {
			t.Fatal(err)
		}
	}
	requests, err := evaluationNotificationRequests([]alerteval.Action{{Kind: alerteval.ActionHealth}})
	if err != nil || len(requests) != 0 {
		t.Fatal(requests, err)
	}
	for _, kind := range []string{"error", "no_data", "recovered"} {
		request, err := evaluationHealthNotificationRequest(model.AlertEvaluationEvent{Kind: kind})
		if err != nil || request.Data["evaluation.status"] != kind {
			t.Fatal(request, err)
		}
	}
}
