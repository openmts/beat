package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

func TestQueryBucketedMetricsAggregatesAndSkipsEmptyBuckets(t *testing.T) {
	mts := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	for _, sample := range []struct {
		offset time.Duration
		value  float64
	}{{0, 10}, {time.Second, 20}, {2 * time.Second, 30}, {4 * time.Second, 40}} {
		if err := mts.WriteMetric(t.Context(), "node", "cpu", sample.value, from.Add(sample.offset)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := mts.QueryBucketedMetrics(t.Context(), BucketedMetricRange{
		Metrics: []string{"cpu"}, Aggregations: map[string]string{"cpu": "avg"}, NodeID: "node",
		From: from, To: from.Add(6 * time.Second), Interval: 2 * time.Second, Buckets: 3})
	if err != nil {
		t.Fatal(err)
	}
	points := result["cpu"]
	if len(points) != 3 {
		t.Fatalf("points = %#v, want three non-empty buckets", points)
	}
	if points[0].Value != 15 || points[1].Value != 30 || points[2].Value != 40 {
		t.Fatalf("bucket values = %#v", points)
	}
	if !points[0].Timestamp.Equal(from) || !points[2].Timestamp.Equal(from.Add(4*time.Second)) {
		t.Fatalf("bucket timestamps = %#v", points)
	}
}

func TestQueryBucketedMetricsRejectsOutOfBudgetRequests(t *testing.T) {
	mts := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	nine := make([]string, 0, metricquery.MaximumSeries+1)
	for range metricquery.MaximumSeries + 1 {
		nine = append(nine, "cpu")
	}
	tests := []struct {
		name    string
		request BucketedMetricRange
	}{
		{name: "too many metrics", request: BucketedMetricRange{Metrics: nine, NodeID: "node",
			From: from, To: from.Add(time.Minute), Interval: time.Second, Buckets: 60}},
		{name: "duplicate metric", request: BucketedMetricRange{Metrics: []string{"cpu", "cpu"}, NodeID: "node",
			From: from, To: from.Add(time.Minute), Interval: time.Second, Buckets: 60}},
		{name: "unknown metric key", request: BucketedMetricRange{Metrics: []string{"bad key"}, NodeID: "node",
			From: from, To: from.Add(time.Minute), Interval: time.Second, Buckets: 60}},
		{name: "sub second interval", request: BucketedMetricRange{Metrics: []string{"cpu"}, NodeID: "node",
			From: from, To: from.Add(time.Minute), Interval: time.Millisecond, Buckets: 60}},
		{name: "bucket budget exceeded", request: BucketedMetricRange{Metrics: []string{"cpu"}, NodeID: "node",
			From: from, To: from.Add(time.Minute), Interval: time.Second, Buckets: metricquery.MaximumPoints + 1}},
		{name: "reversed range", request: BucketedMetricRange{Metrics: []string{"cpu"}, NodeID: "node",
			From: from, To: from, Interval: time.Second, Buckets: 60}},
		{name: "range beyond maximum", request: BucketedMetricRange{Metrics: []string{"cpu"}, NodeID: "node",
			From: from, To: from.Add(32 * 24 * time.Hour), Interval: time.Hour, Buckets: 600}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mts.QueryBucketedMetrics(t.Context(), test.request); !errors.Is(err, metricquery.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
	closed := metricQueryMTS(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := closed.QueryBucketedMetrics(t.Context(), BucketedMetricRange{Metrics: []string{"cpu"}, NodeID: "node",
		From: from, To: from.Add(time.Minute), Interval: time.Second, Buckets: 60})
	if !errors.Is(err, ErrMTSStoreClosed) && !errors.Is(err, metricquery.ErrUnavailable) {
		t.Fatalf("closed store error = %v", err)
	}
}
