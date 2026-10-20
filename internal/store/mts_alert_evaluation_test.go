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

func TestAlertEvidenceReaderCancellationBoundsAndCorruption(t *testing.T) {
	metrics := metricQueryMTS(t)
	at := time.Now().UTC()
	input := ingestInput(at, 100)
	acceptIngest(t, metrics, input)
	request := AlertEvidenceRequest{NodeID: "node", Metric: "cpu"}
	snapshot, err := metrics.ReadAlertEvidence(t.Context(), request)
	if err != nil || len(snapshot.Points) != 1 || snapshot.Points[0].ID != input.Report.SampleID {
		t.Fatal(snapshot, err)
	}
	invalid := request
	invalid.WindowSeconds = alerteval.MaximumWindowSeconds + 1
	if _, err := metrics.ReadAlertEvidence(t.Context(), invalid); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.ReadAlertEvidence(ctx, request); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := metrics.alertReceiptRange(ctx, rawMetricSelection{nodeID: "node", start: at.Add(-time.Second), end: at.Add(time.Second)}); err == nil {
		t.Fatal("receipt query cancellation ignored")
	}
	if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: "custom_bad", Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}, Timestamp: at.UnixNano()}}); err != nil {
		t.Fatal(err)
	}
	request.Metric = "custom_bad"
	if _, err := metrics.ReadAlertEvidence(t.Context(), request); err == nil {
		t.Fatal("wrong scalar type accepted")
	}
}

func TestAlertEvidenceReaderBoundedNodeLockAndClosedStorage(t *testing.T) {
	metrics := metricQueryMTS(t)
	request := AlertEvidenceRequest{NodeID: "node", Metric: "cpu"}
	lock := metrics.nodeLock("node")
	lock.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := metrics.ReadAlertEvidence(ctx, request); err == nil {
		t.Fatal("node lock ignored deadline")
	}
	lock.Unlock()
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.ReadAlertEvidence(t.Context(), request); err == nil {
		t.Fatal("closed storage looked empty")
	}
}

func TestAlertProvenanceNeverInventsMissingReceiptOrMetadata(t *testing.T) {
	at := time.Now()
	points := []TimePoint{{Timestamp: at, Value: 10}}
	receipts := map[int64]model.AgentReceipt{}
	request := AlertEvidenceRequest{Metric: "cpu"}
	if result, err := bindAlertEvidence(points, receipts, request); err != nil || len(result) != 0 {
		t.Fatal(result, err)
	}
	receipts[at.UnixNano()] = model.AgentReceipt{CapturedAt: at, CustomSamplesJSON: "broken"}
	request.Metric = "custom_load"
	if _, err := bindAlertEvidence(points, receipts, request); err == nil {
		t.Fatal("corrupt metadata accepted")
	}
	metrics := metricQueryMTS(t)
	row := receiptTestRow(t)
	row.Fields["sample_id"] = mts.StringValue("invalid")
	if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: model.AgentReceiptMeasurement, Tags: row.Tags, Fields: row.Fields, Timestamp: row.Timestamp}}); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.alertReceiptRange(t.Context(), rawMetricSelection{nodeID: "node", start: time.Unix(0, row.Timestamp).Add(-time.Second), end: time.Unix(0, row.Timestamp).Add(time.Second)}); err == nil {
		t.Fatal("corrupt stored receipt accepted")
	}
}
