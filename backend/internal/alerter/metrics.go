package alerter

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func (a *Alerter) metricValue(
	ctx context.Context,
	metric string,
	node *model.Node,
) (float64, bool, error) {
	switch metric {
	case model.MetricHeartbeatAgeSeconds:
		return a.heartbeatAgeSeconds(node)
	case model.MetricTrafficUsagePercent:
		return a.trafficUsagePercent(ctx, node)
	}
	latest, err := a.mtsStore.QueryLatest(ctx, node.ID)
	if err != nil {
		return 0, false, err
	}
	value, found := latest[metric]
	return value, found, nil
}

func (a *Alerter) heartbeatAgeSeconds(node *model.Node) (float64, bool, error) {
	if node.LastSeen.IsZero() {
		return 0, false, nil
	}
	age := a.now().Sub(node.LastSeen)
	if age < 0 {
		age = 0
	}
	return age.Seconds(), true, nil
}

func (a *Alerter) trafficUsagePercent(
	ctx context.Context,
	node *model.Node,
) (float64, bool, error) {
	if node.TrafficLimit == 0 {
		return 0, false, nil
	}
	now := a.now()
	start, end := model.BillingPeriod(now, node.TrafficResetDay)
	totals, err := a.mtsStore.QueryTrafficUsage(ctx, node.ID, start, end)
	if err != nil {
		return 0, false, fmt.Errorf("query traffic usage: %w", err)
	}
	if totals.TrackedSince == nil {
		return 0, false, nil
	}
	summary, err := model.SummarizeTraffic(model.TrafficPolicy{
		Limit: node.TrafficLimit, LimitType: node.TrafficLimitType, ResetDay: node.TrafficResetDay,
	}, totals, now)
	if err != nil {
		return 0, false, fmt.Errorf("summarize traffic usage: %w", err)
	}
	if summary.Percentage == nil {
		return 0, false, nil
	}
	return *summary.Percentage, true, nil
}

func evaluateThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case "gt", ">":
		return value > threshold
	case "lt", "<":
		return value < threshold
	default:
		return false
	}
}

func formatAlertMessage(rule *model.AlertRule, node *model.Node, value float64) string {
	if rule.Metric == model.MetricHeartbeatAgeSeconds {
		return fmt.Sprintf("[%s] %s: node %s is offline, heartbeat age=%.0fs (limit=%.0fs)",
			rule.Severity, rule.Name, node.Name, value, rule.Threshold)
	}
	return fmt.Sprintf("[%s] %s: %s on node %s, value=%.2f (threshold=%.2f %s)",
		rule.Severity, rule.Name, rule.Metric, node.Name, value, rule.Threshold, rule.Operator)
}

func formatRecoveryMessage(rule *model.AlertRule, node *model.Node, value float64) string {
	if rule.Metric == model.MetricHeartbeatAgeSeconds {
		return fmt.Sprintf("[resolved] %s: node %s recovered, heartbeat age=%.0fs",
			rule.Name, node.Name, value)
	}
	return fmt.Sprintf("[resolved] %s: %s on node %s recovered, value=%.2f",
		rule.Name, rule.Metric, node.Name, value)
}
