package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type transactionalAlertQueue struct {
	database      *sql.DB
	request       notificationqueue.Request
	afterMutation error
}

func (queue *transactionalAlertQueue) Enqueue(ctx context.Context, request notificationqueue.Request, mutation func(*sql.Tx) error) (notificationqueue.Receipt, error) {
	transaction, err := queue.database.BeginTx(ctx, nil)
	if err != nil {
		return notificationqueue.Receipt{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := mutation(transaction); err != nil {
		if errors.Is(err, notificationqueue.ErrSkipMutation) {
			return notificationqueue.Receipt{Skipped: true}, nil
		}
		return notificationqueue.Receipt{}, err
	}
	if queue.afterMutation != nil {
		return notificationqueue.Receipt{}, queue.afterMutation
	}
	if err := transaction.Commit(); err != nil {
		return notificationqueue.Receipt{}, err
	}
	queue.request = request
	return notificationqueue.Receipt{MessageID: "queued", State: "pending"}, nil
}

func newAlertNotificationFixture(t *testing.T) (*AlertNotificationService, *transactionalAlertQueue, model.AlertEvent) {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	queue := &transactionalAlertQueue{database: database.DB}
	event := model.AlertEvent{ID: uuid.NewString(), RuleID: "rule", NodeID: "node", Message: "high CPU", Value: 92.5,
		Status: model.AlertStatusTriggered, TriggeredAt: time.Now().UTC(), RuleName: "CPU", NodeName: "edge", Metric: "cpu", Severity: model.AlertSeverityCritical}
	return NewAlertNotificationService(store.NewAlertEventStore(database.DB), queue), queue, event
}

func TestAlertNotificationTriggerPersistsOnceAndUsesStableIdentity(t *testing.T) {
	service, queue, event := newAlertNotificationFixture(t)
	receipt, err := service.Trigger(t.Context(), &event)
	if err != nil || receipt.State != "pending" {
		t.Fatalf("trigger: %+v/%v", receipt, err)
	}
	stored, err := service.events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored == nil || stored.Status != model.AlertStatusTriggered {
		t.Fatal("event not persisted")
	}
	if queue.request.SourceKey != "alert:"+event.ID+":triggered" || queue.request.Data["node.name"] != "edge" || queue.request.Data["metric.value"] != "92.5" {
		t.Fatalf("request=%+v", queue.request)
	}
	event.ID = uuid.NewString()
	if duplicate, err := service.Trigger(t.Context(), &event); err != nil || !duplicate.Skipped {
		t.Fatalf("active episode was duplicated: %+v/%v", duplicate, err)
	}
}

func TestAlertNotificationFailureRollsBackEventAndAudit(t *testing.T) {
	service, queue, event := newAlertNotificationFixture(t)
	want := errors.New("queue write failure")
	queue.afterMutation = want
	if _, err := service.Trigger(t.Context(), &event); !errors.Is(err, want) {
		t.Fatal(err)
	}
	stored, err := service.events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored != nil {
		t.Fatal("failed queue retained event")
	}
	var count int
	if err := queue.database.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE resource_id=?", event.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed queue retained success audit")
	}
}

func TestAlertNotificationTransitionCompareAndSwap(t *testing.T) {
	service, queue, event := newAlertNotificationFixture(t)
	if _, err := service.Trigger(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	now := event.TriggeredAt.Add(time.Minute)
	event.Status, event.AcknowledgedAt = model.AlertStatusAcknowledged, &now
	change := store.AlertEventTransition{Event: event, Previous: model.AlertStatusTriggered}
	queue.afterMutation = errors.New("queue full")
	if _, err := service.Transition(t.Context(), change); err == nil {
		t.Fatal("queue failure lost")
	}
	stored, err := service.events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored.Status != model.AlertStatusTriggered {
		t.Fatal("failed queue changed state")
	}
	queue.afterMutation = nil
	if _, err := service.Transition(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	if queue.request.EventKind != "alert.acknowledged" || queue.request.SourceKey != "alert:"+event.ID+":acknowledged" {
		t.Fatal("ack source identity incorrect")
	}
	if receipt, err := service.Transition(t.Context(), change); err != nil || !receipt.Skipped {
		t.Fatalf("stale change: %+v/%v", receipt, err)
	}
}

func TestAlertNotificationRejectsInvalidDependenciesAndEvents(t *testing.T) {
	service, queue, event := newAlertNotificationFixture(t)
	if _, err := service.Trigger(t.Context(), nil); !errors.Is(err, store.ErrInvalidAlertTransition) {
		t.Fatal(err)
	}
	for _, unavailable := range []*AlertNotificationService{NewAlertNotificationService(nil, queue), NewAlertNotificationService(service.events, nil)} {
		if _, err := unavailable.Trigger(t.Context(), &event); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
			t.Fatal(err)
		}
		if _, err := unavailable.Transition(t.Context(), store.AlertEventTransition{}); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
			t.Fatal(err)
		}
	}
	event.ID = ""
	if _, err := service.Trigger(t.Context(), &event); !errors.Is(err, store.ErrInvalidAlertTransition) {
		t.Fatal(err)
	}
	if _, err := service.Transition(t.Context(), store.AlertEventTransition{}); !errors.Is(err, store.ErrInvalidAlertTransition) {
		t.Fatal(err)
	}
}

func TestAlertNotificationKindsAndOccurrenceTimes(t *testing.T) {
	now := time.Now().UTC()
	ack, resolved := now.Add(time.Minute), now.Add(2*time.Minute)
	tests := []struct {
		metric string
		status model.AlertStatus
		kind   string
		at     time.Time
	}{
		{"cpu", model.AlertStatusTriggered, "alert.triggered", now},
		{"cpu", model.AlertStatusAcknowledged, "alert.acknowledged", ack},
		{"cpu", model.AlertStatusResolved, "alert.resolved", resolved},
		{model.MetricHeartbeatAgeSeconds, model.AlertStatusTriggered, "availability.offline", now},
		{model.MetricHeartbeatAgeSeconds, model.AlertStatusResolved, "availability.recovered", resolved},
		{model.MetricTrafficUsagePercent, model.AlertStatusTriggered, "traffic.quota", now},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			request := alertNotificationRequest(model.AlertEvent{Metric: test.metric, Status: test.status, TriggeredAt: now, AcknowledgedAt: &ack, ResolvedAt: &resolved})
			if request.EventKind != test.kind || request.Data["event.occurred_at"] != test.at.Format(time.RFC3339Nano) || !request.UseSubscriptions {
				t.Fatalf("unexpected request: %+v", request)
			}
		})
	}
}
