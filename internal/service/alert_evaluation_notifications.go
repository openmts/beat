package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type EvaluationCommitter interface {
	Commit(context.Context, store.AlertEvaluationCommit) error
	CommitTx(context.Context, *sql.Tx, store.AlertEvaluationCommit) error
}

type AlertEvaluationNotificationService struct {
	states EvaluationCommitter
	queue  notificationqueue.BatchEnqueuer
}

func NewAlertEvaluationNotificationService(states EvaluationCommitter, queue notificationqueue.BatchEnqueuer) *AlertEvaluationNotificationService {
	return &AlertEvaluationNotificationService{states: states, queue: queue}
}

func (s *AlertEvaluationNotificationService) Commit(ctx context.Context, change store.AlertEvaluationCommit) ([]notificationqueue.Receipt, error) {
	if s.states == nil {
		return nil, alerteval.ErrUnavailable
	}
	requests, err := evaluationNotificationRequests(change.Decision.Actions)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, s.states.Commit(ctx, change)
	}
	if s.queue == nil {
		return nil, notificationqueue.ErrQueueUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	return s.queue.EnqueueBatch(ctx, requests, func(tx *sql.Tx) error { return s.states.CommitTx(ctx, tx, change) })
}

func evaluationNotificationRequests(actions []alerteval.Action) ([]notificationqueue.Request, error) {
	if len(actions) > notificationqueue.MaximumBatchMessages {
		return nil, alerteval.ErrConflict
	}
	requests := make([]notificationqueue.Request, 0, len(actions))
	for _, action := range actions {
		if !action.Notify {
			continue
		}
		switch action.Kind {
		case alerteval.ActionThresholdTrigger, alerteval.ActionThresholdRecovery, alerteval.ActionDeviceUnavailable:
			if action.Event == nil {
				return nil, alerteval.ErrConflict
			}
			requests = append(requests, alertNotificationRequest(*action.Event))
		case alerteval.ActionHealth:
			if action.Health == nil {
				return nil, alerteval.ErrConflict
			}
			request, err := evaluationHealthNotificationRequest(*action.Health)
			if err != nil {
				return nil, err
			}
			requests = append(requests, request)
		default:
			return nil, alerteval.ErrConflict
		}
	}
	return requests, nil
}

func evaluationHealthNotificationRequest(event model.AlertEvaluationEvent) (notificationqueue.Request, error) {
	kinds := map[string]string{"no_data": "alert.no_data", "error": "alert.evaluation_error", "recovered": "alert.evaluation_recovered"}
	kind, ok := kinds[event.Kind]
	if !ok {
		return notificationqueue.Request{}, alerteval.ErrConflict
	}
	text := event.Reason
	if event.DeviceName != "" {
		text += " · " + event.DeviceName
	}
	return notificationqueue.Request{SourceKey: event.SourceKey, SourceKind: "alert_evaluation", SourceID: event.ID,
		EventKind: kind, Subject: "[Beat] Alert evaluation " + event.Kind, Text: text, UseSubscriptions: true,
		Data: map[string]string{"event.status": event.Kind, "event.occurred_at": event.OccurredAt.Format(time.RFC3339Nano),
			"rule.id": event.RuleID, "node.id": event.NodeID, "evaluation.status": event.Kind, "evaluation.reason": event.Reason, "evaluation.epoch": event.EpochID}}, nil
}
