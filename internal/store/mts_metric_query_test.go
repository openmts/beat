package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

func metricQueryMTS(t *testing.T) *MTSStore {
	t.Helper()
	mts, err := NewMTSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mts.Close() })
	return mts
}

func TestMTSMetricQueryReturnsAlignedGapsAndActualAvailability(t *testing.T) {
	mts := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	for _, sample := range []struct {
		offset time.Duration
		value  float64
	}{{-time.Hour, 99}, {time.Second, 10}, {10 * time.Second, 20}, {50 * time.Second, 0}, {time.Minute, 999}} {
		if err := mts.WriteMetric(t.Context(), "node", "cpu", sample.value, from.Add(sample.offset)); err != nil {
			t.Fatal(err)
		}
	}
	values, err := mts.ReadMetricQuery(t.Context(), metricquery.Scan{NodeID: "node", Metric: "cpu", Aggregation: "avg", From: from, To: from.Add(time.Minute), Interval: 20 * time.Second, Buckets: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(values.Points) != 3 || values.Points[0].Value == nil || *values.Points[0].Value != 15 || values.Points[1].Value != nil || values.Points[2].Value == nil || *values.Points[2].Value != 0 {
		t.Fatalf("query values=%v", values)
	}
	if values.AvailableFrom == nil || !values.AvailableFrom.Equal(from.Add(-time.Hour)) {
		t.Fatal("available_from was replaced with requested range")
	}
}

func TestMTSMetricQueryCounterRateIncludesPriorObservation(t *testing.T) {
	mts := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	for _, sample := range []struct {
		offset time.Duration
		value  float64
	}{{-time.Second, 100}, {time.Second, 120}, {11 * time.Second, 5}, {51 * time.Second, 85}} {
		if err := mts.WriteMetric(t.Context(), "node", "net_recv_total", sample.value, from.Add(sample.offset)); err != nil {
			t.Fatal(err)
		}
	}
	values, err := mts.ReadMetricQuery(t.Context(), metricquery.Scan{NodeID: "node", Metric: "net_recv_total", Aggregation: "rate", From: from, To: from.Add(time.Minute), Interval: 20 * time.Second, Buckets: 3})
	if err != nil || values.Points[0].Value == nil || math.Abs(*values.Points[0].Value-25.0/12) > 1e-12 {
		t.Fatalf("rate=%v/%v", values, err)
	}
}

func TestMTSMetricQuerySeparatesEmptyClosedAndCanceled(t *testing.T) {
	mts := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	plan := metricquery.Scan{NodeID: "node", Metric: "cpu", Aggregation: "avg", From: from, To: from.Add(time.Minute), Interval: time.Minute, Buckets: 1}
	values, err := mts.ReadMetricQuery(t.Context(), plan)
	if err != nil || len(values.Points) != 1 || values.Points[0].Value != nil {
		t.Fatalf("empty result=%v/%v", values, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mts.ReadMetricQuery(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled query returned empty success")
	}
	if err := mts.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := mts.ReadMetricQuery(t.Context(), plan); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed query returned empty success")
	}
}

// 边缘探测（available_from / rate 前值）有独立预算：它耗光时按「未知」退化，
// 而不是让整次查询以 metric query capacity exceeded 失败。
func shrinkMetricQueryEdgeBudget(t *testing.T, budget int) {
	t.Helper()
	previous := metricQueryEdgeSampleBudget
	metricQueryEdgeSampleBudget = budget
	t.Cleanup(func() { metricQueryEdgeSampleBudget = previous })
}

func TestMTSMetricQueryKeepsRangeWhenEdgeProbeRunsOutOfBudget(t *testing.T) {
	mts := metricQueryMTS(t)
	shrinkMetricQueryEdgeBudget(t, 1)
	from := time.Unix(1_700_000_000, 0).UTC()
	for _, offset := range []time.Duration{-time.Hour, time.Second, time.Minute} {
		if err := mts.WriteMetric(t.Context(), "node", "cpu", 5, from.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	values, err := mts.ReadMetricQuery(t.Context(), metricquery.Scan{NodeID: "node", Metric: "cpu", Aggregation: "avg",
		From: from, To: from.Add(time.Minute), Interval: 20 * time.Second, Buckets: 3})
	if err != nil {
		t.Fatalf("requested range must survive a budgeted edge probe, got %v", err)
	}
	if values.AvailableFrom != nil {
		t.Fatalf("available_from = %v, want unknown", values.AvailableFrom)
	}
	if len(values.Points) != 3 {
		t.Fatalf("points = %v, want three aligned buckets", values.Points)
	}
}

func TestMTSMetricQueryRateKeepsRangeWhenPreviousProbeRunsOutOfBudget(t *testing.T) {
	mts := metricQueryMTS(t)
	shrinkMetricQueryEdgeBudget(t, 1)
	from := time.Unix(1_700_000_000, 0).UTC()
	// 前一个观测分散在四个小时分片里：探测必须跨分片合并，一行预算不够用。
	for _, offset := range []time.Duration{-4 * time.Hour, -3 * time.Hour, -2 * time.Hour, -time.Hour} {
		if err := mts.WriteMetric(t.Context(), "node", "net_recv_total", 100, from.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	for _, offset := range []time.Duration{2 * time.Second, 22 * time.Second, 42 * time.Second} {
		if err := mts.WriteMetric(t.Context(), "node", "net_recv_total", 160, from.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	values, err := mts.ReadMetricQuery(t.Context(), metricquery.Scan{NodeID: "node", Metric: "net_recv_total", Aggregation: "rate",
		From: from, To: from.Add(time.Minute), Interval: 20 * time.Second, Buckets: 3})
	if err != nil {
		t.Fatalf("requested range must survive a budgeted previous-observation probe, got %v", err)
	}
	if len(values.Points) != 3 || values.Points[0].Value != nil {
		t.Fatalf("rate points = %v, want an empty first bucket", values.Points)
	}
	if values.Points[1].Value == nil || values.Points[2].Value == nil {
		t.Fatalf("rate points = %v, want the remaining buckets intact", values.Points)
	}
}
