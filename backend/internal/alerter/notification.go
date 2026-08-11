package alerter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (a *Alerter) triggerAlert(
	ctx context.Context,
	rule *model.AlertRule,
	node *model.Node,
	value float64,
) error {
	activeEvent, err := a.alertEventStore.GetActiveEvent(ctx, rule.ID, node.ID)
	if err != nil {
		return fmt.Errorf("get active event: %w", err)
	}
	if activeEvent != nil {
		return nil
	}
	event := &model.AlertEvent{
		ID: uuid.New().String(), RuleID: rule.ID, NodeID: node.ID,
		Message: formatAlertMessage(rule, node, value), Value: value,
		Status: model.AlertStatusTriggered, TriggeredAt: a.now(),
	}
	if err := a.alertEventStore.CreateEvent(ctx, event); err != nil {
		return fmt.Errorf("create event: %w", err)
	}
	if err := a.pushToChannels(ctx, event); err != nil {
		return fmt.Errorf("push to channels: %w", err)
	}
	slog.InfoContext(ctx, "alert triggered", "rule_id", rule.ID, "node_id", node.ID, "value", value)
	return nil
}

func (a *Alerter) resolveAlert(
	ctx context.Context,
	rule *model.AlertRule,
	node *model.Node,
	value float64,
) error {
	activeEvent, err := a.alertEventStore.GetActiveEvent(ctx, rule.ID, node.ID)
	if err != nil {
		return fmt.Errorf("get active event: %w", err)
	}
	if activeEvent == nil {
		return nil
	}
	now := a.now()
	activeEvent.Status = model.AlertStatusResolved
	activeEvent.ResolvedAt = &now
	if err := a.alertEventStore.UpdateEvent(ctx, activeEvent); err != nil {
		return fmt.Errorf("update event: %w", err)
	}
	recovery := *activeEvent
	recovery.Message = formatRecoveryMessage(rule, node, value)
	recovery.Value = value
	if err := a.pushToChannels(ctx, &recovery); err != nil {
		return fmt.Errorf("push recovery to channels: %w", err)
	}
	slog.InfoContext(ctx, "alert resolved", "rule_id", rule.ID, "node_id", node.ID, "value", value)
	return nil
}

func (a *Alerter) pushToChannels(ctx context.Context, event *model.AlertEvent) error {
	channels, err := a.alertChannelStore.ListEnabledChannels(ctx)
	if err != nil {
		return fmt.Errorf("list enabled channels: %w", err)
	}
	for _, channel := range channels {
		if _, err := a.delivery.Send(ctx, &channel, event); err != nil {
			slog.ErrorContext(ctx, "alert notification failed", "channel_id", channel.ID,
				"channel_type", channel.ChannelType, "error", err)
		}
	}
	return nil
}
