package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

func TestReadMetricLatestReturnsNewestSampleAndEmptyState(t *testing.T) {
	s := setupTestMTS(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.WriteMetric(ctx, "node-latest", "cpu", 12.5, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteMetric(ctx, "node-latest", "cpu", 42.5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := s.ReadMetricLatest(ctx, metricquery.LatestScan{NodeID: "node-latest", Metric: "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	if !value.Found || value.Value != 42.5 || value.SampleAt.IsZero() {
		t.Fatalf("unexpected latest value: %+v", value)
	}
	empty, err := s.ReadMetricLatest(ctx, metricquery.LatestScan{NodeID: "node-latest", Metric: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Found || empty.Value != 0 || !empty.SampleAt.IsZero() {
		t.Fatalf("missing sample reported as present: %+v", empty)
	}
}
