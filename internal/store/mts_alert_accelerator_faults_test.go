package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type gpuEvidenceRows struct {
	rows    []mts.Row
	index   int
	failure error
}

func (r *gpuEvidenceRows) Next() bool { r.index++; return r.index <= len(r.rows) }

func (r *gpuEvidenceRows) Row() mts.Row { return r.rows[r.index-1] }

func (r *gpuEvidenceRows) Err() error { return r.failure }

func (r *gpuEvidenceRows) Close() error { return nil }

func TestAcceleratorReceiptStreamRejectsCorruptIdentity(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	report := f.report().Accelerators
	point, err := acceleratorSamplePoint(request.NodeID, *report)
	if err != nil {
		t.Fatal(err)
	}
	selection := acceleratorEvidenceRange{request: request, from: report.CapturedAt.Add(-time.Minute), to: report.CapturedAt.Add(time.Minute)}
	row := mts.Row{Timestamp: point.Timestamp, Tags: point.Tags, Fields: point.Fields}
	for _, rows := range [][]mts.Row{{row, row}, {{Timestamp: row.Timestamp, Tags: map[string]string{"node": "other"}, Fields: row.Fields}}, {{Timestamp: row.Timestamp, Tags: row.Tags, Fields: map[string]mts.FieldValue{}}}} {
		if _, err := consumeAcceleratorReceipts(t.Context(), &gpuEvidenceRows{rows: rows}, selection); !errors.Is(err, alerteval.ErrInvalidObservation) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := consumeAcceleratorReceipts(ctx, &gpuEvidenceRows{rows: []mts.Row{row}}, selection); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	failure := errors.New("iterator unavailable")
	if _, err := consumeAcceleratorReceipts(t.Context(), &gpuEvidenceRows{failure: failure}, selection); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	outside := row
	outside.Timestamp = selection.to.Add(time.Second).UnixNano()
	if result, err := consumeAcceleratorReceipts(t.Context(), &gpuEvidenceRows{rows: []mts.Row{outside}}, selection); err != nil || len(result) != 0 {
		t.Fatal(result, err)
	}
}

func TestAcceleratorValueStreamRejectsWrongDeviceAndScalarTypes(t *testing.T) {
	_, request := gpuEvidenceFixture(t)
	at := request.AcceleratorSnapshot.CapturedAt
	selection := acceleratorEvidenceRange{request: request, from: at.Add(-time.Second), to: at}
	row := mts.Row{Timestamp: at.UnixNano(), Tags: map[string]string{"node": request.NodeID, "device": request.DeviceID, "vendor": request.DeviceVendor}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(1)}}
	for _, rows := range [][]mts.Row{{row, row}, {{Timestamp: row.Timestamp, Tags: map[string]string{"node": request.NodeID}, Fields: row.Fields}}, {{Timestamp: row.Timestamp, Tags: row.Tags, Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}}}} {
		input := acceleratorValueRange{selection: selection, key: request.Metric, values: map[int64]map[string]float64{}}
		if err := consumeAcceleratorValues(&gpuEvidenceRows{rows: rows}, input); err == nil {
			t.Fatal("invalid GPU scalar stream accepted")
		}
	}
	outside := row
	outside.Timestamp = at.Add(time.Nanosecond).UnixNano()
	input := acceleratorValueRange{selection: selection, key: request.Metric, values: map[int64]map[string]float64{}}
	if err := consumeAcceleratorValues(&gpuEvidenceRows{rows: []mts.Row{outside}}, input); err != nil || len(input.values) != 0 {
		t.Fatal(input.values, err)
	}
}

func TestAcceleratorAlertEvidenceValidatesPairedValuesAndCadence(t *testing.T) {
	_, request := gpuEvidenceFixture(t)
	at := request.AcceleratorSnapshot.CapturedAt.UnixNano()
	selection := acceleratorEvidenceRange{request: request}
	receipts := map[int64]acceleratorEvidenceReceipt{at: {id: "sample", present: true, cadence: request.AcceleratorSnapshot.CadenceMS}}
	values := map[int64]map[string]float64{at: {"gpu_memory_used_bytes": 20, "gpu_memory_total_bytes": 10}}
	selection.request.Metric = model.MetricGPUMemoryPercent
	if _, err := bindAcceleratorAlertPoints(selection, receipts, values); !errors.Is(err, alerteval.ErrInvalidObservation) {
		t.Fatal(err)
	}
	values[at] = map[string]float64{"gpu_memory_used_bytes": 1}
	if points, err := bindAcceleratorAlertPoints(selection, receipts, values); err != nil || len(points) != 0 {
		t.Fatal(points, err)
	}
	values[at] = map[string]float64{}
	if points, err := bindAcceleratorAlertPoints(selection, receipts, values); err != nil || len(points) != 0 {
		t.Fatal(points, err)
	}
}

func TestAcceleratorReceiptRejectsReusedIDAndOldPolicy(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	first := f.report().Accelerators
	point, err := acceleratorSamplePoint(request.NodeID, *first)
	if err != nil {
		t.Fatal(err)
	}
	row := mts.Row{Timestamp: point.Timestamp, Tags: point.Tags, Fields: point.Fields}
	second := *first
	second.CapturedAt = first.CapturedAt.Add(time.Second)
	next, err := acceleratorSamplePoint(request.NodeID, second)
	if err != nil {
		t.Fatal(err)
	}
	selection := acceleratorEvidenceRange{request: request, from: first.CapturedAt.Add(-time.Second), to: second.CapturedAt}
	if _, err := consumeAcceleratorReceipts(t.Context(), &gpuEvidenceRows{rows: []mts.Row{row, {Timestamp: next.Timestamp, Tags: next.Tags, Fields: next.Fields}}}, selection); !errors.Is(err, alerteval.ErrInvalidObservation) {
		t.Fatal(err)
	}
	copy := *request.AcceleratorSnapshot
	copy.PolicyRevision++
	selection.request.AcceleratorSnapshot = &copy
	if rows, err := consumeAcceleratorReceipts(t.Context(), &gpuEvidenceRows{rows: []mts.Row{row}}, selection); err != nil || len(rows) != 0 {
		t.Fatal("old-policy samples admitted", rows, err)
	}
}

func TestAcceleratorAlertHelpersPropagateQueryFailures(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	at := request.AcceleratorSnapshot.CapturedAt
	invalid := acceleratorEvidenceRange{request: request, from: at.Add(time.Second), to: at}
	if _, err := f.metrics.acceleratorAlertReceipts(t.Context(), invalid); err == nil {
		t.Fatal("reversed metadata range accepted")
	}
	if _, err := f.metrics.acceleratorAlertValues(t.Context(), invalid); err == nil {
		t.Fatal("reversed value range accepted")
	}
	if err := f.metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.metrics.ReadAlertEvidence(t.Context(), request); err == nil {
		t.Fatal("closed public GPU read looked empty")
	}
}
