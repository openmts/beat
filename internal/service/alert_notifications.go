package service

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type AlertNotificationService struct {
	events *store.AlertEventStore
	queue  notificationqueue.Enqueuer
}

func NewAlertNotificationService(events *store.AlertEventStore, queue notificationqueue.Enqueuer) *AlertNotificationService {
	return &AlertNotificationService{events: events, queue: queue}
}

func (service *AlertNotificationService) Trigger(ctx context.Context, event *model.AlertEvent) (notificationqueue.Receipt, error) {
	if event == nil {
		return notificationqueue.Receipt{}, store.ErrInvalidAlertTransition
	}
	if service.queue == nil || service.events == nil {
		return notificationqueue.Receipt{}, notificationqueue.ErrQueueUnavailable
	}
	return service.queue.Enqueue(ctx, alertNotificationRequest(*event), func(transaction *sql.Tx) error {
		created, err := service.events.CreateActiveEventTx(ctx, transaction, event)
		if err != nil {
			return err
		}
		if !created {
			return notificationqueue.ErrSkipMutation
		}
		return nil
	})
}

func (service *AlertNotificationService) Transition(ctx context.Context, change store.AlertEventTransition) (notificationqueue.Receipt, error) {
	if service.queue == nil || service.events == nil {
		return notificationqueue.Receipt{}, notificationqueue.ErrQueueUnavailable
	}
	return service.queue.Enqueue(ctx, alertNotificationRequest(change.Event), func(transaction *sql.Tx) error {
		changed, err := service.events.TransitionEventTx(ctx, transaction, change)
		if err != nil {
			return err
		}
		if !changed {
			return notificationqueue.ErrSkipMutation
		}
		return nil
	})
}

func alertNotificationRequest(event model.AlertEvent) notificationqueue.Request {
	kind := alertNotificationKind(event)
	return notificationqueue.Request{SourceKey: "alert:" + event.ID + ":" + string(event.Status), SourceKind: "alert", SourceID: event.ID,
		EventKind: kind, Subject: "[Beat] Alert " + string(event.Status), Text: event.Message, UseSubscriptions: true,
		Data: map[string]string{
			"event.status": string(event.Status), "event.severity": string(event.Severity), "event.message": event.Message,
			"event.occurred_at": alertNotificationTime(event).Format(time.RFC3339Nano),
			"node.id":           event.NodeID, "node.name": event.NodeName, "rule.id": event.RuleID, "rule.name": event.RuleName,
			"metric.name": event.Metric, "metric.value": strconv.FormatFloat(event.Value, 'f', -1, 64),
			"metric.formatted_value": strconv.FormatFloat(event.Value, 'f', 2, 64),
		}}
}

func alertNotificationKind(event model.AlertEvent) string {
	if event.Status == model.AlertStatusAcknowledged {
		return "alert.acknowledged"
	}
	if event.Metric == model.MetricHeartbeatAgeSeconds {
		if event.Status == model.AlertStatusResolved {
			return "availability.recovered"
		}
		return "availability.offline"
	}
	if event.Status == model.AlertStatusResolved {
		return "alert.resolved"
	}
	if event.Metric == model.MetricTrafficUsagePercent {
		return "traffic.quota"
	}
	return "alert.triggered"
}

func alertNotificationTime(event model.AlertEvent) time.Time {
	if event.Status == model.AlertStatusResolved && event.ResolvedAt != nil {
		return *event.ResolvedAt
	}
	if event.Status == model.AlertStatusAcknowledged && event.AcknowledgedAt != nil {
		return *event.AcknowledgedAt
	}
	return event.TriggeredAt
}
