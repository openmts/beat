package store

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestWriteNodeMetricsDerivesTrafficDeltas(t *testing.T) {
	store := setupTestMTS(t)
	ctx := t.Context()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	samples := []struct {
		received float64
		sent     float64
	}{
		{received: 100, sent: 200},
		{received: 150, sent: 260},
		{received: 150, sent: 260},
		{received: 20, sent: 30},
	}
	for index, sample := range samples {
		err := store.WriteNodeMetrics(ctx, NodeMetricSample{
			NodeID: "node",
			Metrics: model.NodeMetrics{
				NetRecvTotal: sample.received,
				NetSentTotal: sample.sent,
			},
			Timestamp: start.Add(time.Duration(index) * time.Minute),
		})
		if err != nil {
			t.Fatalf("write sample %d: %v", index, err)
		}
	}
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("flush traffic deltas: %v", err)
	}

	got, err := store.QueryMetrics(
		ctx,
		[]string{trafficReceivedDelta, trafficSentDelta},
		start.Add(-time.Second),
		start.Add(4*time.Minute),
		"node",
	)
	if err != nil {
		t.Fatalf("query traffic deltas: %v", err)
	}
	assertPointValues(t, got[trafficReceivedDelta], []float64{0, 50, 0, 20})
	assertPointValues(t, got[trafficSentDelta], []float64{0, 60, 0, 30})
}

func TestQueryTrafficUsageUsesOnlyRequestedPeriod(t *testing.T) {
	store := setupTestMTS(t)
	ctx := t.Context()
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	points := []struct {
		timestamp time.Time
		received  float64
		sent      float64
	}{
		{start.Add(-time.Minute), 5, 7},
		{start, 10, 20},
		{start.Add(time.Hour), 30, 40},
		{start.Add(2 * time.Hour), 50, 60},
	}
	for _, point := range points {
		if err := store.writeMetricValues(ctx, "node", []model.MetricValue{
			{Name: trafficReceivedDelta, Value: point.received},
			{Name: trafficSentDelta, Value: point.sent},
		}, point.timestamp); err != nil {
			t.Fatalf("write traffic point: %v", err)
		}
	}
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("flush traffic points: %v", err)
	}

	totals, err := store.QueryTrafficUsage(ctx, "node", start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("query traffic usage: %v", err)
	}
	if totals.Received != 40 || totals.Sent != 60 {
		t.Fatalf("traffic totals = %#v, want received 40 sent 60", totals)
	}
	if totals.TrackedSince == nil || !totals.TrackedSince.Equal(start) {
		t.Fatalf("tracked since = %v, want %v", totals.TrackedSince, start)
	}
}

func TestTrafficQueriesReturnStorageErrors(t *testing.T) {
	store := setupTestMTS(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.QueryTrafficUsage(
		ctx,
		"node",
		time.Now().Add(-time.Hour),
		time.Now(),
	); err == nil {
		t.Fatal("expected traffic query error after close")
	}
}

func assertPointValues(t *testing.T, points []TimePoint, expected []float64) {
	t.Helper()
	if len(points) != len(expected) {
		t.Fatalf("point count = %d, want %d: %#v", len(points), len(expected), points)
	}
	for index, value := range expected {
		if points[index].Value != value {
			t.Fatalf("point %d = %v, want %v", index, points[index].Value, value)
		}
	}
}
