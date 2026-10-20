package store

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func receiptTestRow(t *testing.T) mts.Row {
	t.Helper()
	input := ingestInput(time.Now(), 100)
	digest, err := input.Report.Digest()
	if err != nil {
		t.Fatal(err)
	}
	point := agentReceiptPoint("node", model.AgentReceipt{SampleID: input.Report.SampleID, CapturedAt: input.Report.CapturedAt, PayloadDigest: digest,
		CredentialFingerprint: input.Identity.CredentialFingerprint, ProtocolVersion: model.AgentReportProtocol, ReportIntervalMS: 1000})
	return mts.Row{Measurement: point.Measurement, Tags: point.Tags, Fields: point.Fields, Timestamp: point.Timestamp}
}

func TestAgentReceiptRejectsCorruptTypedIdentityWithoutNumericFallback(t *testing.T) {
	base := receiptTestRow(t)
	for _, mutate := range []func(*mts.Row){
		func(r *mts.Row) { r.Tags = map[string]string{"node": "node", "sample": "high-cardinality"} },
		func(r *mts.Row) { r.Timestamp = 0 }, func(r *mts.Row) { delete(r.Fields, "sample_id") },
		func(r *mts.Row) { r.Fields["sample_id"] = mts.Int64Value(1) }, func(r *mts.Row) { r.Fields["sample_id"] = mts.StringValue("bad") },
		func(r *mts.Row) { r.Fields["payload_digest"] = mts.StringValue(strings.Repeat("x", 64)) },
		func(r *mts.Row) { r.Fields["credential_fingerprint"] = mts.StringValue("short") },
		func(r *mts.Row) { r.Fields["protocol_version"] = mts.Int64Value(1) },
		func(r *mts.Row) { r.Fields["report_interval_ms"] = mts.Int64Value(0) },
	} {
		row := base
		row.Fields = maps.Clone(base.Fields)
		mutate(&row)
		if _, err := DecodeAgentReceipt(row); !errors.Is(err, ErrAgentReceiptInvalid) {
			t.Fatal("invalid receipt admitted", err)
		}
	}
	metrics := metricQueryMTS(t)
	base.Fields["sample_id"] = mts.StringValue("corrupt")
	if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: base.Measurement, Tags: base.Tags, Fields: base.Fields, Timestamp: base.Timestamp, Precision: mts.PrecisionNanosecond}}); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.LatestAgentReceipt(t.Context(), "node"); !errors.Is(err, ErrAgentReceiptInvalid) {
		t.Fatal("corrupt receipt became no data", err)
	}
	if _, err := metrics.QueryAgentSnapshot(t.Context(), "node", nil); !errors.Is(err, ErrAgentReceiptInvalid) {
		t.Fatal("corrupt snapshot became healthy", err)
	}
}

func TestAgentAdmissionPolicyFailuresReleaseAllPendingResources(t *testing.T) {
	metrics := metricQueryMTS(t)
	input := ingestInput(time.Now(), 100)
	for _, scenario := range []string{"invalid report", "invalid identity", "unbound custom", "catalog error", "missing counter"} {
		request := input
		var policy AgentMetricPolicy
		switch scenario {
		case "invalid report":
			request.Report.ProtocolVersion = 1
		case "invalid identity":
			request.Identity.CredentialFingerprint = ""
		case "unbound custom":
			request.Report.CustomMetrics = map[string]float64{"unbound": 1}
		case "catalog error":
			policy = func(context.Context) (MetricReportSample, error) {
				return MetricReportSample{}, errors.New("catalog unavailable")
			}
		case "missing counter":
			policy = func(context.Context) (MetricReportSample, error) {
				return MetricReportSample{EnabledBuiltins: map[string]bool{trafficReceivedDelta: true}}, nil
			}
		}
		if attempt, err := metrics.PrepareAgentReport(t.Context(), request, policy); err == nil || attempt != nil {
			t.Fatal("failed policy was admitted", scenario)
		}
		if len(metrics.agentPrepared) != 0 {
			t.Fatal("failed policy leaked prepared capacity")
		}
	}
}

func TestAgentSnapshotAndReceiptQueriesRejectClosedCancelledAndMalformedPlans(t *testing.T) {
	metrics := metricQueryMTS(t)
	input := ingestInput(time.Now(), 100)
	acceptIngest(t, metrics, input)
	snapshot, err := metrics.QueryAgentSnapshot(t.Context(), "node", nil)
	if err != nil || snapshot.Metrics["cpu"] != 25 || snapshot.Receipt == nil {
		t.Fatal("committed snapshot missing", err)
	}
	if _, err := metrics.readAgentReceipt(t.Context(), agentReceiptQuery{nodeID: "node", end: -1}); err == nil {
		t.Fatal("invalid receipt range reached engine")
	}
	if _, _, err := metrics.queryLatestScalar(t.Context(), latestScalarQuery{nodeID: "node", end: time.Now()}); err == nil {
		t.Fatal("invalid scalar selection reached engine")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.readAgentReceipt(ctx, agentReceiptQuery{nodeID: "node", end: time.Now().UnixNano()}); err == nil {
		t.Fatal("cancelled receipt query returned success")
	}
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.QueryAgentSnapshot(t.Context(), "node", nil); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed snapshot became empty success")
	}
	if _, err := metrics.BeginMetricSnapshot(t.Context()); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed MTS snapshot opened")
	}
}

func TestAgentSnapshotRejectsWrongScalarAndReleasedBackupGuard(t *testing.T) {
	metrics := metricQueryMTS(t)
	if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: "cpu", Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}, Timestamp: time.Now().UnixNano()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.QueryAgentSnapshot(t.Context(), "node", nil); err == nil {
		t.Fatal("wrong scalar type converted to zero")
	}
	guard, err := metrics.BeginMetricSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	guard.Close()
	guard.Close()
	if _, err := guard.ExportCatalogRows(t.Context(), MetricExport{Cutoff: time.Now()}, func(mts.Row) error { return nil }); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("released snapshot guard remained usable")
	}
}
