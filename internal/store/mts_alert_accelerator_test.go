package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func gpuEvidenceFixture(t *testing.T) (*acceleratorFixture, AlertEvidenceRequest) {
	t.Helper()
	f := newAcceleratorFixture(t)
	report := f.report()
	attempt := f.prepare(t, report)
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if err := f.publish(t); err != nil {
		t.Fatal(err)
	}
	snapshot := report.Accelerators.Inventory()
	digest, err := report.Accelerators.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request := AlertEvidenceRequest{NodeID: f.node.ID, DeviceID: model.AcceleratorDeviceID(f.node.ID, snapshot.Devices[0].Fingerprint), DeviceVendor: snapshot.Devices[0].Vendor, Metric: "gpu_utilization_percent", AcceleratorSnapshot: &snapshot, AcceleratorDigest: digest, PolicyRevision: 1}
	return f, request
}

func TestAcceleratorAlertEvidenceExactNativeTimeAndDevice(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	at := request.AcceleratorSnapshot.CapturedAt.UnixNano()
	points := []mts.Point{{Measurement: request.Metric, Tags: map[string]string{"node": request.NodeID, "device": request.DeviceID, "vendor": request.DeviceVendor}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(99)}, Timestamp: at + 1},
		{Measurement: request.Metric, Tags: map[string]string{"node": request.NodeID, "device": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "vendor": request.DeviceVendor}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(98)}, Timestamp: at}}
	if err := f.metrics.ImportPoints(t.Context(), points); err != nil {
		t.Fatal(err)
	}
	result, err := f.metrics.ReadAlertEvidence(t.Context(), request)
	if err != nil || result.Accelerator == nil || result.Receipt != nil || len(result.Points) != 1 || result.Points[0].Value != 73.125 {
		t.Fatalf("wrong native binding: %+v %v", result, err)
	}
	if !result.Points[0].At.Equal(request.AcceleratorSnapshot.CapturedAt) || result.Cadence != 5*time.Second {
		t.Fatal("host time or adjacent ns changed native evidence")
	}
	for _, metric := range []string{model.MetricGPUMemoryPercent, model.MetricGPUPowerLimitPercent} {
		request.Metric = metric
		result, err = f.metrics.ReadAlertEvidence(t.Context(), request)
		if err != nil || len(result.Points) != 1 {
			t.Fatal(result, err)
		}
		expected := 25.0
		if metric == model.MetricGPUPowerLimitPercent {
			expected = 49.2
		}
		if math.Abs(result.Points[0].Value-expected) > 1e-9 {
			t.Fatal(result.Points[0])
		}
	}
}

func TestAcceleratorAlertEvidenceRejectsMissingAndChangedProof(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	missing := request
	missing.AcceleratorSnapshot = nil
	if result, err := f.metrics.ReadAlertEvidence(t.Context(), missing); err != nil || len(result.Points) != 0 || result.Receipt != nil {
		t.Fatal(result, err)
	}
	changed := request
	changed.AcceleratorDigest = "mismatch"
	if _, err := f.metrics.ReadAlertEvidence(t.Context(), changed); !errors.Is(err, alerteval.ErrInvalidObservation) {
		t.Fatal(err)
	}
	changed = request
	changed.DeviceID = "invalid"
	if _, err := f.metrics.ReadAlertEvidence(t.Context(), changed); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	changed = request
	changed.WindowSeconds = alerteval.MaximumWindowSeconds + 1
	if _, err := f.metrics.ReadAlertEvidence(t.Context(), changed); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.metrics.ReadAlertEvidence(ctx, request); err == nil {
		t.Fatal("canceled GPU read succeeded")
	}
}

func TestAcceleratorAlertEvidenceDoesNotZeroFillMissingRatio(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	request.Metric = model.MetricGPUPowerLimitPercent
	if err := f.metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: "gpu_power_limit_watts", Tags: map[string]string{"node": request.NodeID, "device": request.DeviceID, "vendor": request.DeviceVendor}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(0)}, Timestamp: request.AcceleratorSnapshot.CapturedAt.UnixNano()}}); err != nil {
		t.Fatal(err)
	}
	result, err := f.metrics.ReadAlertEvidence(t.Context(), request)
	if err != nil || len(result.Points) != 0 {
		t.Fatal("zero denominator synthesized a ratio", result, err)
	}
}
