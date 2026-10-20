package alerter

import (
	"context"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type testEventWriter interface {
	CreateEvent(context.Context, *model.AlertEvent) error
	UpdateEvent(context.Context, *model.AlertEvent) error
}

type testAlertNotifications struct {
	store   testEventWriter
	err     error
	receipt notificationqueue.Receipt
	events  []model.AlertEvent
}

func newTestAlerter(rules alertRuleStore, events alertEventStore, channels *mockAlertChannelStore, metrics mtsStore, nodes nodeStore) *Alerter {
	writer, _ := events.(testEventWriter)
	notifications := &testAlertNotifications{store: writer}
	if channels != nil {
		notifications.err = channels.err
	}
	return New(Config{Rules: rules, Events: events, Metrics: metrics, Nodes: nodes, Notifications: notifications})
}

func (fixture *testAlertNotifications) Trigger(ctx context.Context, event *model.AlertEvent) (notificationqueue.Receipt, error) {
	if fixture.err != nil || fixture.receipt.Skipped || fixture.receipt.Duplicate {
		return fixture.receipt, fixture.err
	}
	if err := fixture.store.CreateEvent(ctx, event); err != nil {
		return notificationqueue.Receipt{}, err
	}
	fixture.events = append(fixture.events, *event)
	return fixture.receipt, nil
}

func (fixture *testAlertNotifications) Transition(ctx context.Context, change store.AlertEventTransition) (notificationqueue.Receipt, error) {
	if fixture.err != nil || fixture.receipt.Skipped || fixture.receipt.Duplicate {
		return fixture.receipt, fixture.err
	}
	if err := fixture.store.UpdateEvent(ctx, &change.Event); err != nil {
		return notificationqueue.Receipt{}, err
	}
	fixture.events = append(fixture.events, change.Event)
	return fixture.receipt, nil
}
