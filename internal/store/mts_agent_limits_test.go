package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAgentPreparedBatchSurvivesCancelledWriteWaitWithoutPolicyDrift(t *testing.T) {
	metrics := metricQueryMTS(t)
	input := ingestInput(time.Now().UTC(), 100)
	policyCalls := 0
	policy := func(context.Context) (MetricReportSample, error) {
		policyCalls++
		return MetricReportSample{EnabledBuiltins: map[string]bool{"cpu": true}}, nil
	}
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, policy)
	if err != nil {
		t.Fatal(err)
	}
	metrics.engine.writeMu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, err = attempt.Commit(ctx)
	cancel()
	metrics.engine.writeMu.Unlock()
	attempt.Close()
	if !errors.Is(err, context.DeadlineExceeded) || metrics.engine.poisoned.Load() {
		t.Fatal("known pre-write cancellation poisoned the WAL", err)
	}
	retry, err := metrics.PrepareAgentReport(t.Context(), input, func(context.Context) (MetricReportSample, error) {
		policyCalls++
		return MetricReportSample{EnabledBuiltins: map[string]bool{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retry.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	retry.Close()
	if policyCalls != 1 || len(metrics.agentPrepared) != 0 {
		t.Fatal("prepared policy was replaced or retained after commit")
	}
	values, err := metrics.QueryLatest(t.Context(), "node")
	if err != nil || values["cpu"] != 25 {
		t.Fatal("original admitted policy did not finish", err)
	}
}

func TestAgentPreparedCapacityExpiresByAdmissionTimeAndReleasesUnwrittenPlans(t *testing.T) {
	metrics := metricQueryMTS(t)
	now := time.Now()
	metrics.agentPrepared = make(map[string]*preparedAgentReport)
	for index := range maximumPreparedAgentReports {
		metrics.agentPrepared[fmt.Sprintf("old-%d", index)] = &preparedAgentReport{admittedAt: now, uncertain: true, receipt: model.AgentReceipt{CapturedAt: now.Add(model.MaximumAgentClockSkew)}}
	}
	input := ingestInput(now, 100)
	if attempt, err := metrics.PrepareAgentReport(t.Context(), input, nil); !errors.Is(err, ErrAgentIngestCapacity) || attempt != nil {
		t.Fatal("unbounded prepared cache", err)
	}
	input.Now = now.Add(model.MaximumAgentSampleAge + time.Nanosecond)
	input.Report.CapturedAt = input.Now
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal("expired admitted cache did not free capacity", err)
	}
	if len(metrics.agentPrepared) != 1 {
		t.Fatal("cache expiry used future capture time")
	}
	attempt.Close()
	attempt.Close()
	if len(metrics.agentPrepared) != 0 {
		t.Fatal("cancelled/unwritten plan leaked capacity")
	}
	if _, err := attempt.Commit(t.Context()); !errors.Is(err, ErrAgentIngestClosed) {
		t.Fatal("closed attempt was writable")
	}
}

func TestAgentIngestNodeLockSnapshotAndCancelledEntryDoNotMutate(t *testing.T) {
	metrics := metricQueryMTS(t)
	input := ingestInput(time.Now(), 100)
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := metrics.PrepareAgentReport(t.Context(), input, nil); !errors.Is(err, ErrAgentIngestCapacity) || duplicate != nil {
		t.Fatal("node serialization bypassed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	if _, err := metrics.QueryAgentSnapshot(ctx, "node", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("snapshot ignored node-lock cancellation", err)
	}
	cancel()
	attempt.Close()
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.PrepareAgentReport(ctx, input, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request admitted", err)
	}
	if _, err := metrics.QueryAgentSnapshot(ctx, "node", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled snapshot admitted", err)
	}
	if len(metrics.agentPrepared) != 0 {
		t.Fatal("cancelled operation created a plan")
	}
}

func TestAgentIngestionMetricsHaveFixedLabelsAndNonnegativeAges(t *testing.T) {
	metrics := metricQueryMTS(t)
	for _, ack := range []model.AgentReportACK{{MetricsAccepted: true}, {MetricsAccepted: true, Duplicate: true}, {ContactAccepted: true},
		{ErrorCode: "ingestion_capacity"}, {ErrorCode: "sample_out_of_order"}, {ErrorCode: "sample_conflict"}, {ErrorCode: "clock_skew"},
		{ErrorCode: "sample_expired"}, {ErrorCode: "storage_unavailable"}, {ErrorCode: "private-node private-token"}} {
		metrics.ObserveAgentReport(ack)
	}
	metrics.ObserveAgentReport(model.AgentReportACK{Retryable: true, CapturedAt: time.Now().Add(time.Minute)})
	metrics.ObserveAgentReport(model.AgentReportACK{Retryable: true, CapturedAt: time.Now().Add(-time.Minute)})
	output := metrics.AgentIngestPrometheus()
	if strings.Contains(output, "private") || strings.Contains(output, "node=") || strings.Contains(output, "sample=") {
		t.Fatal("high-cardinality/private metric label")
	}
	for _, outcome := range agentIngestOutcomes {
		if !strings.Contains(output, fmt.Sprintf("outcome=%q", outcome)) {
			t.Fatal("missing bounded outcome", outcome)
		}
	}
	metrics.engine.poisoned.Store(true)
	if !strings.Contains(metrics.AgentIngestPrometheus(), "beat_agent_ingestion_recovery_required 1") {
		t.Fatal("poisoned state not observable")
	}
}
