package alerter

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrTelemetryUnavailable = errors.New("telemetry is unavailable or stale for alert evaluation")

type agentReceiptReader interface {
	LatestAgentReceipt(context.Context, string) (*model.AgentReceipt, error)
}

type agentOfflineWriter interface {
	MarkNodeStaleOffline(context.Context, string, time.Time) error
}

func (a *Alerter) contactOfflineAfter(ctx context.Context, nodeID string) (time.Duration, error) {
	reader, ok := a.mtsStore.(agentReceiptReader)
	if !ok {
		return a.offlineAfter, nil
	}
	receipt, err := reader.LatestAgentReceipt(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	if receipt == nil {
		return a.offlineAfter, nil
	}
	return max(a.offlineAfter, model.AgentOfflineAfter(time.Duration(receipt.ReportIntervalMS)*time.Millisecond)), nil
}

func (a *Alerter) markStaleAgentNodes(ctx context.Context) error {
	writer, hasWriter := a.nodeStore.(agentOfflineWriter)
	_, hasReader := a.mtsStore.(agentReceiptReader)
	if !hasWriter || !hasReader {
		_, err := a.nodeStore.MarkStaleNodesOffline(ctx, a.now().Add(-a.offlineAfter))
		return err
	}
	nodes, err := a.nodeStore.ListNodes(ctx, "")
	if err != nil {
		return err
	}
	now := a.now()
	cutoffs := make(map[string]time.Time)
	for _, node := range nodes {
		if node.Status != model.NodeStatusOnline || node.LastSeen.IsZero() || !node.LastSeen.Before(now.Add(-a.offlineAfter)) {
			continue
		}
		delay, err := a.contactOfflineAfter(ctx, node.ID)
		if err != nil {
			return err
		}
		cutoffs[node.ID] = now.Add(-delay)
	}
	for nodeID, cutoff := range cutoffs {
		if err := writer.MarkNodeStaleOffline(ctx, nodeID, cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (a *Alerter) freshAlertRule(ctx context.Context, rule *model.AlertRule, node *model.Node) (*model.AlertRule, error) {
	if rule.Metric == model.MetricHeartbeatAgeSeconds {
		delay, err := a.contactOfflineAfter(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		result := *rule
		if rule.Operator == "gt" || rule.Operator == ">" {
			result.Threshold = max(result.Threshold, delay.Seconds())
		}
		return &result, nil
	}
	reader, ok := a.mtsStore.(agentReceiptReader)
	if !ok {
		return rule, nil
	}
	receipt, err := reader.LatestAgentReceipt(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, ErrTelemetryUnavailable
	}
	freshness := model.NewTelemetryFreshness(a.now(), &receipt.CapturedAt, time.Duration(receipt.ReportIntervalMS)*time.Millisecond)
	if freshness.TelemetryState != model.TelemetryFresh {
		return nil, ErrTelemetryUnavailable
	}
	return rule, nil
}
