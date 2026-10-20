package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func TestBeginMetricErasureRejectsUnknownScope(t *testing.T) {
	metrics := metricQueryMTS(t)
	if _, err := metrics.BeginMetricErasure(t.Context(), "not-a-scope", "", ""); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("unknown scope error = %v", err)
	}
	if _, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureNodeHistory, "", ""); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("node scope without node id error = %v", err)
	}
	if _, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureDeleteNetworkTask, "", ""); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("task scope without task id error = %v", err)
	}
}

func TestBeginMetricErasureFreezesPlanCopies(t *testing.T) {
	metrics := metricQueryMTS(t)
	session, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureNodeHistory, "node-1", "")
	if err != nil || session == nil {
		t.Fatalf("begin node erasure = %v err = %v", session, err)
	}
	plan := session.Plan()
	if plan.NodeID != "node-1" || len(plan.Digest) != 64 || plan.CutoffUnixNano <= 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Measurements) != len(sortUniqueMeasurements(managedMeasurements())) {
		t.Fatalf("plan measurements = %v", plan.Measurements)
	}
	plan.Measurements[0] = "mutated"
	if session.Plan().Measurements[0] == "mutated" {
		t.Fatal("Plan exposed the internal slice")
	}
	session.Close()
	session.Close()
	if err := session.Erase(t.Context(), plan.Measurements[0]); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("erase after close error = %v", err)
	}
	var nilSession *MetricErasureSession
	if err := nilSession.Erase(t.Context(), "cpu"); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("nil session erase error = %v", err)
	}
	nilSession.Close()
}

func TestMetricErasurePlanSelectionPerScope(t *testing.T) {
	metrics := metricQueryMTS(t)
	task, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureNetworkTaskHistory, "", "task-9")
	if err != nil {
		t.Fatalf("begin task erasure: %v", err)
	}
	if selected := task.Plan().Measurements; len(selected) != 1 || selected[0] != networkProbeMeasurement {
		t.Fatalf("task plan = %v", selected)
	}
	task.Close()
	fleet, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureFleetTelemetry, "", "")
	if err != nil {
		t.Fatalf("begin fleet erasure: %v", err)
	}
	for _, measurement := range fleet.Plan().Measurements {
		if measurement == networkProbeMeasurement {
			t.Fatalf("fleet plan kept the network probe: %v", fleet.Plan().Measurements)
		}
	}
	fleet.Close()
	all, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureAllMetricHistory, "", "")
	if err != nil {
		t.Fatalf("begin all-history erasure: %v", err)
	}
	if len(all.Plan().Measurements) == 0 {
		t.Fatal("all-history plan is empty")
	}
	all.Close()
}

func TestMetricErasureSessionErasesMeasurement(t *testing.T) {
	metrics := metricQueryMTS(t)
	at := time.Unix(1_700_200_000, 0).UTC()
	if err := metrics.WriteMetric(t.Context(), "node-1", "cpu", 42, at); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	session, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureNodeHistory, "node-1", "")
	if err != nil {
		t.Fatalf("begin erasure: %v", err)
	}
	if err := session.Erase(t.Context(), "cpu"); err != nil {
		t.Fatalf("erase measurement: %v", err)
	}
	session.Close()
	if err := metrics.CompactMetricErasure(t.Context()); err != nil {
		t.Fatalf("compact after erasure: %v", err)
	}
}

func TestMetricErasureRejectsUnregisteredMeasurement(t *testing.T) {
	metrics := metricQueryMTS(t)
	points := []mts.Point{{Measurement: "rogue_measurement", Tags: map[string]string{"node": "node-1"},
		Fields: map[string]mts.FieldValue{"value": mts.Float64Value(1)}, Timestamp: time.Now().Unix(),
		Precision: mts.PrecisionSecond}}
	if err := metrics.engine.Write(t.Context(), points, mts.WriteOptions{}); err != nil {
		t.Fatalf("write rogue measurement: %v", err)
	}
	if _, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureFleetTelemetry, "", ""); !errors.Is(err, ErrMetricErasureInventory) {
		t.Fatalf("unregistered measurement error = %v", err)
	}
}

func TestMetricErasureOperationsRequireOpenStore(t *testing.T) {
	metrics := metricQueryMTS(t)
	if err := metrics.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureFleetTelemetry, "", ""); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("closed begin error = %v", err)
	}
	if err := metrics.CompactMetricErasure(t.Context()); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("closed compact error = %v", err)
	}
}

func TestMetricErasureOperationsHonorContext(t *testing.T) {
	metrics := metricQueryMTS(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.BeginMetricErasure(cancelled, model.MetricErasureFleetTelemetry, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled begin error = %v", err)
	}
	if err := metrics.CompactMetricErasure(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled compact error = %v", err)
	}
}
