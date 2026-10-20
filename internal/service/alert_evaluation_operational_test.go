package service

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestEvaluationHeartbeatObservesContactAndGrace(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	input := alerteval.StepInput{Rule: alerteval.DefaultRule(), Node: *f.node, Observation: model.AlertEvaluationObservation{ObservedAt: f.at}}
	input.Rule.Metric = model.MetricHeartbeatAgeSeconds
	input.Node.LastSeen = time.Time{}
	if err := f.evaluator.observeHeartbeat(t.Context(), &input); err != nil || input.Observation.Reason != "never_seen" {
		t.Fatal(input, err)
	}
	input.Node.LastSeen = f.at.Add(-time.Minute)
	if err := f.evaluator.observeHeartbeat(t.Context(), &input); err != nil || input.Observation.Value == nil || *input.Observation.Value != 60 {
		t.Fatal(input, err)
	}
	f.report(t, 10, model.CollectedCustomSamples{})
	if _, err := f.evaluator.observe(t.Context(), &input); err != nil || input.Rule.Threshold < model.AgentOfflineAfter(time.Second).Seconds() {
		t.Fatal(input, err)
	}
	if err := f.metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.evaluator.observeHeartbeat(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationError {
		t.Fatal(input, err)
	}
}

func TestEvaluationTrafficUsesReceiptAndCatalogPolicies(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	input := alerteval.StepInput{Rule: alerteval.DefaultRule(), Node: *f.node, Observation: model.AlertEvaluationObservation{ObservedAt: f.at}}
	input.Rule.Metric = model.MetricTrafficUsagePercent
	input.Node.TrafficLimit = 0
	if err := f.evaluator.trafficObservation(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationNoData {
		t.Fatal(input, err)
	}
	input.Node.TrafficLimit, input.Node.TrafficLimitType, input.Node.TrafficResetDay = 100, model.TrafficLimitSum, 1
	if err := f.evaluator.trafficObservation(t.Context(), &input); err != nil || input.Observation.Value != nil {
		t.Fatal(input, err)
	}
	f.report(t, 20, model.CollectedCustomSamples{})
	if _, err := f.evaluator.observe(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationOK || input.Observation.Value == nil {
		t.Fatal(input, err)
	}
	input.Node.TrafficLimitType = "invalid"
	if err := f.evaluator.trafficObservation(t.Context(), &input); err == nil {
		t.Fatal("invalid traffic policy accepted")
	}
	if _, err := f.evaluator.observeTraffic(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationError {
		t.Fatal(input, err)
	}
	input.Observation.ObservedAt = f.at.Add(time.Hour)
	if err := f.evaluator.trafficObservation(t.Context(), &input); err != nil || input.Observation.Reason != "stale_receipt" {
		t.Fatal(input, err)
	}
	if _, err := f.db.DB.Exec("UPDATE metric_definitions SET enabled=0 WHERE metric_key='net_recv_delta'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.evaluator.observeTraffic(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationDisabled {
		t.Fatal(input, err)
	}
	if _, err := f.db.DB.Exec("DELETE FROM metric_definitions WHERE metric_key='net_sent_delta'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.evaluator.observeTraffic(t.Context(), &input); err == nil {
		t.Fatal("missing policy ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.evaluator.observeTraffic(ctx, &input); err == nil {
		t.Fatal("cancelled policy read ignored")
	}
	if minTime(f.at.Add(time.Second), f.at) != f.at {
		t.Fatal("end bound")
	}
}
