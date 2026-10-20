package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func gpuObservationInput(t *testing.T, f *gpuEvaluationFixture) *alerteval.StepInput {
	t.Helper()
	rule, err := f.core.evaluator.rules.Get(t.Context(), f.core.rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &alerteval.StepInput{Rule: *rule, Node: *f.core.node, DeviceID: f.devices[0], Observation: model.AlertEvaluationObservation{ObservedAt: f.core.at, RunID: f.core.evaluator.runID}}
}

func TestGPUObservationRejectsStorageAndCatalogFailures(t *testing.T) {
	for _, kind := range []string{"closed metrics", "invalid proof", "disabled metric", "missing metric", "broken catalog", "broken inventory"} {
		t.Run(kind, func(t *testing.T) {
			f := newGPUEvaluationFixture(t)
			report := f.report(t, map[string]float64{"a": 90, "b": 95})
			input := gpuObservationInput(t, f)
			switch kind {
			case "closed metrics":
				if err := f.core.metrics.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid proof":
				encoded, err := json.Marshal(report.Inventory())
				if err != nil {
					t.Fatal(err)
				}
				fields := map[string]mts.FieldValue{"sample_id": mts.StringValue(report.SampleID), "payload_digest": mts.StringValue(strings.Repeat("b", 64)), "cadence_ms": mts.Int64Value(report.CadenceMS), "inventory": mts.StringValue(string(encoded))}
				if err := f.core.metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: model.AcceleratorSampleMeasurement, Tags: map[string]string{"node": f.core.node.ID}, Fields: fields, Timestamp: report.CapturedAt.UnixNano()}}); err != nil {
					t.Fatal(err)
				}
			case "disabled metric":
				if _, err := f.core.db.DB.Exec("UPDATE metric_definitions SET enabled=0 WHERE metric_key='gpu_utilization_percent'"); err != nil {
					t.Fatal(err)
				}
			case "missing metric":
				if _, err := f.core.db.DB.Exec("DELETE FROM metric_definitions WHERE metric_key='gpu_utilization_percent'"); err != nil {
					t.Fatal(err)
				}
			case "broken catalog":
				if _, err := f.core.db.DB.Exec("ALTER TABLE metric_definitions RENAME COLUMN metric_key TO missing_key"); err != nil {
					t.Fatal(err)
				}
			case "broken inventory":
				if _, err := f.core.db.DB.Exec("DROP TABLE node_accelerator_state"); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.core.evaluator.observeAccelerator(t.Context(), input)
			if kind == "missing metric" {
				if !errors.Is(err, alerteval.ErrConflict) {
					t.Fatal(err)
				}
				return
			}
			if kind == "broken catalog" || kind == "broken inventory" {
				if err == nil {
					t.Fatal("metadata failure hidden")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := model.EvaluationError
			if kind == "disabled metric" {
				expected = model.EvaluationDisabled
			}
			if input.Observation.Status != expected || input.Observation.Value != nil {
				t.Fatal(input.Observation)
			}
		})
	}
}

func TestGPUPreviewUsesPerDeviceTargetsWithoutPersistingState(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	f.report(t, map[string]float64{"a": 90, "b": 95})
	rule, err := f.core.evaluator.rules.Get(t.Context(), f.core.rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := model.AlertEvaluationPreviewRequest{Rule: *rule, NodeIDs: []string{f.core.node.ID}}
	previews, err := f.core.evaluator.Preview(t.Context(), "client", request)
	if err != nil || len(previews) != 2 {
		t.Fatal(previews, err)
	}
	if previews[0].DeviceID == previews[1].DeviceID || previews[0].DeviceName == "" || previews[1].DeviceName == "" {
		t.Fatal("preview lost device dimension")
	}
	request.Rule.Scope.DeviceID = f.devices[1]
	previews, err = f.core.evaluator.Preview(t.Context(), "client", request)
	if err != nil || len(previews) != 1 || previews[0].DeviceID != f.devices[1] || previews[0].Value == nil || *previews[0].Value != 95 {
		t.Fatal(previews, err)
	}
	var count int
	if err := f.core.db.DB.QueryRow("SELECT count(*) FROM alert_evaluation_states").Scan(&count); err != nil || count != 0 {
		t.Fatal("preview persisted evaluation state", err)
	}
	request.Rule.Metric = model.MetricGPUMemoryPercent
	previews, err = f.core.evaluator.Preview(t.Context(), "client", request)
	if err != nil || len(previews) != 1 || *previews[0].Value != 25 {
		t.Fatal(previews, err)
	}
}

func TestGPUInvalidNumericPolicyIsVisible(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	f.report(t, map[string]float64{"a": 90, "b": 95})
	input := gpuObservationInput(t, f)
	input.Rule.Aggregation = "unsupported"
	if _, err := f.core.evaluator.observeAccelerator(t.Context(), input); err != nil || input.Observation.Status != model.EvaluationError || input.Observation.Reason != "invalid_policy" {
		t.Fatal(input.Observation, err)
	}
	if err := f.core.db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.evaluator.observeAccelerator(t.Context(), input); err == nil {
		t.Fatal("closed database accepted")
	}
}
