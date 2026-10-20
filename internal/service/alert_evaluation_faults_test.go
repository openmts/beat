package service

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestEvaluationMetadataFailuresDoNotAdvanceOrRetainLeases(t *testing.T) {
	for _, table := range []string{"alert_rules", "nodes", "alert_events", "metric_definitions"} {
		t.Run(table, func(t *testing.T) {
			f := newEvaluationRuntimeFixture(t)
			if _, err := f.evaluator.states.Admit(t.Context(), f.at); err != nil {
				t.Fatal(err)
			}
			pairs, err := f.evaluator.Due(t.Context(), alerteval.DueQuery{At: f.at, Limit: 1})
			if err != nil || len(pairs) != 1 {
				t.Fatal(pairs, err)
			}
			if _, err := f.db.DB.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			if err := f.evaluator.Evaluate(t.Context(), pairs[0]); err == nil {
				t.Fatal("metadata failure hidden")
			}
			if pairs, err := f.evaluator.Due(t.Context(), alerteval.DueQuery{At: f.at, Limit: 1}); err != nil || len(pairs) != 1 {
				t.Fatal("lease retained", pairs, err)
			}
		})
	}
}

func TestEvaluationMetadataMissingAndInvalidPolicies(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	key := store.AlertEvaluationKey{RuleID: f.rule.ID, NodeID: f.node.ID}
	if _, _, err := f.evaluator.prepare(t.Context(), key); err == nil {
		t.Fatal("missing state ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := f.evaluator.prepare(ctx, key); err == nil {
		t.Fatal("cancelled state read ignored")
	}
	input := alerteval.StepInput{Rule: alerteval.DefaultRule(), Node: *f.node}
	input.Rule.Metric = "missing"
	if _, err := f.evaluator.metricReference(t.Context(), &input); err == nil {
		t.Fatal("missing metric policy ignored")
	}
	input.Rule.Metric = "cpu"
	input.Rule.Name = ""
	if _, err := f.evaluator.observe(t.Context(), &input); err != nil || input.Observation.Status != model.EvaluationError {
		t.Fatal(input, err)
	}
	input.Rule.Metric = "custom_load"
	if _, err := f.evaluator.customReference(ctx, input, store.AlertMetricPolicyReference{}); err == nil {
		t.Fatal("cancelled binding read ignored")
	}
}
