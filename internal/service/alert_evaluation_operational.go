package service

import (
	"context"
	"fmt"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (e *AlertEvaluator) observeHeartbeat(ctx context.Context, input *alerteval.StepInput) error {
	observation := &input.Observation
	observation.Status, observation.Reason = model.EvaluationNoData, "never_seen"
	if input.Node.LastSeen.IsZero() {
		return nil
	}
	receipt, err := e.metrics.LatestAgentReceipt(ctx, input.Node.ID)
	if err != nil {
		observation.Status, observation.Reason = model.EvaluationError, "storage_unavailable"
		return nil
	}
	cadence := model.MinimumAgentReportInterval
	if receipt != nil {
		cadence = time.Duration(receipt.ReportIntervalMS) * time.Millisecond
	}
	if alerteval.CanonicalOperator(input.Rule.Operator) == "gt" {
		input.Rule.Threshold = max(input.Rule.Threshold, model.AgentOfflineAfter(cadence).Seconds())
	}
	value := max(observation.ObservedAt.Sub(input.Node.LastSeen), 0).Seconds()
	observation.Status, observation.Reason = model.EvaluationOK, ""
	observation.Value = &value
	observation.SourceAt, observation.Cadence = observation.ObservedAt, cadence
	observation.EvidenceID = "heartbeat:" + input.Node.ID + ":" + observation.ObservedAt.Format(time.RFC3339Nano)
	return nil
}

func (e *AlertEvaluator) observeTraffic(ctx context.Context, input *alerteval.StepInput) ([]store.AlertMetricPolicyReference, error) {
	references := make([]store.AlertMetricPolicyReference, 0, 2)
	disabled := false
	for _, key := range []string{"net_recv_delta", "net_sent_delta"} {
		definition, err := e.catalog.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if definition == nil {
			return nil, alerteval.ErrConflict
		}
		references = append(references, store.AlertMetricPolicyReference{Key: key, Revision: definition.Revision})
		disabled = disabled || !definition.Enabled
	}
	if disabled {
		input.Observation.Status, input.Observation.Reason = model.EvaluationDisabled, "metric_disabled"
		return references, nil
	}
	if err := e.trafficObservation(ctx, input); err != nil {
		input.Observation.Status, input.Observation.Reason = model.EvaluationError, "storage_unavailable"
		input.Observation.Value = nil
	}
	return references, nil
}

func (e *AlertEvaluator) trafficObservation(ctx context.Context, input *alerteval.StepInput) error {
	observation := &input.Observation
	observation.Status, observation.Reason = model.EvaluationNoData, "traffic_unavailable"
	if input.Node.TrafficLimit == 0 {
		return nil
	}
	receipt, err := e.metrics.LatestAgentReceipt(ctx, input.Node.ID)
	if err != nil || receipt == nil {
		return err
	}
	cadence := time.Duration(receipt.ReportIntervalMS) * time.Millisecond
	if model.NewTelemetryFreshness(observation.ObservedAt, &receipt.CapturedAt, cadence).TelemetryState != model.TelemetryFresh {
		observation.Reason = "stale_receipt"
		return nil
	}
	start, end := model.BillingPeriod(observation.ObservedAt, input.Node.TrafficResetDay)
	end = minTime(end, receipt.CapturedAt.Add(time.Nanosecond))
	if !end.After(start) {
		return nil
	}
	totals, err := e.metrics.ReadAlertTraffic(ctx, store.AlertTrafficRequest{NodeID: input.Node.ID, ReceiptID: receipt.SampleID, Start: start, End: end})
	if err != nil {
		return err
	}
	if totals.TrackedSince == nil {
		return nil
	}
	summary, err := model.SummarizeTraffic(model.TrafficPolicy{Limit: input.Node.TrafficLimit, LimitType: input.Node.TrafficLimitType, ResetDay: input.Node.TrafficResetDay}, totals, observation.ObservedAt)
	if err != nil {
		return err
	}
	if summary.Percentage == nil {
		return nil
	}
	observation.Status, observation.Reason = model.EvaluationOK, ""
	observation.Value = summary.Percentage
	observation.SourceAt, observation.Cadence = receipt.CapturedAt, cadence
	observation.EvidenceID = fmt.Sprintf("traffic:%s:%s:%d", input.Node.ID, receipt.SampleID, input.Rule.Revision)
	return nil
}

func minTime(first, second time.Time) time.Time {
	if first.Before(second) {
		return first
	}
	return second
}
