package alerter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type alertEvaluation struct {
	rule  *model.AlertRule
	node  *model.Node
	value float64
}

func (a *Alerter) triggerAlert(ctx context.Context, evaluation alertEvaluation) error {
	rule, node, value := evaluation.rule, evaluation.node, evaluation.value
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
		RuleName: rule.Name, NodeName: node.Name, Metric: rule.Metric,
		Severity: rule.Severity,
	}
	if a.notifications == nil {
		return notificationqueue.ErrQueueUnavailable
	}
	receipt, err := a.notifications.Trigger(ctx, event)
	if err != nil {
		return fmt.Errorf("queue alert event: %w", err)
	}
	if receipt.Skipped || receipt.Duplicate {
		return nil
	}
	slog.InfoContext(ctx, "alert triggered", "rule_id", rule.ID, "node_id", node.ID, "value", value)
	return nil
}

func (a *Alerter) resolveAlert(ctx context.Context, evaluation alertEvaluation) error {
	rule, node, value := evaluation.rule, evaluation.node, evaluation.value
	activeEvent, err := a.alertEventStore.GetActiveEvent(ctx, rule.ID, node.ID)
	if err != nil {
		return fmt.Errorf("get active event: %w", err)
	}
	if activeEvent == nil {
		return nil
	}
	// Acknowledged events are only cleared manually by the operator, never
	// automatically when the metric recovers.
	if activeEvent.Status == model.AlertStatusAcknowledged {
		return nil
	}
	if a.notifications == nil {
		return notificationqueue.ErrQueueUnavailable
	}
	now := a.now()
	recovery := *activeEvent
	recovery.Status = model.AlertStatusResolved
	recovery.ResolvedAt = &now
	recovery.Message = formatRecoveryMessage(rule, node, value)
	recovery.Value = value
	recovery.RuleName = rule.Name
	recovery.NodeName = node.Name
	recovery.Metric = rule.Metric
	recovery.Severity = rule.Severity
	change := store.AlertEventTransition{Event: recovery, Previous: activeEvent.Status}
	receipt, err := a.notifications.Transition(ctx, change)
	if err != nil {
		return fmt.Errorf("queue alert recovery: %w", err)
	}
	if receipt.Skipped || receipt.Duplicate {
		return nil
	}
	slog.InfoContext(ctx, "alert resolved", "rule_id", rule.ID, "node_id", node.ID, "value", value)
	return nil
}
