package store

import (
	"testing"
	"time"
)

func TestTrafficAccountingBoundaryTracksLatestReceipt(t *testing.T) {
	metrics := metricQueryMTS(t)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if boundary, found, err := metrics.TrafficAccountingBoundary(t.Context(), "node"); err != nil || found {
		t.Fatalf("empty boundary = %v %v %v, want not found", boundary, found, err)
	}
	if acceptIngest(t, metrics, ingestInput(base.Add(100*time.Nanosecond), 100)) {
		t.Fatal("first sample reported duplicate")
	}
	boundary, found, err := metrics.TrafficAccountingBoundary(t.Context(), "node")
	if err != nil || !found || !boundary.Equal(base) {
		t.Fatalf("boundary = %v %v %v, want %v", boundary, found, err, base)
	}
	next := base.Add(2*time.Second + 700*time.Millisecond)
	if acceptIngest(t, metrics, ingestInput(next, 180)) {
		t.Fatal("second sample reported duplicate")
	}
	boundary, found, err = metrics.TrafficAccountingBoundary(t.Context(), "node")
	if err != nil || !found || !boundary.Equal(base.Add(2*time.Second)) {
		t.Fatalf("advanced boundary = %v %v %v", boundary, found, err)
	}
	if _, _, err := metrics.TrafficAccountingBoundary(t.Context(), ""); err == nil {
		t.Fatal("expected error for empty node")
	}
}

func TestQueryTrafficUsageAfterExcludesBoundarySecond(t *testing.T) {
	metrics := metricQueryMTS(t)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	samples := []struct {
		offset   time.Duration
		received float64
	}{{0, 100}, {time.Second, 150}, {2 * time.Second, 200}, {3 * time.Second, 260}}
	for _, sample := range samples {
		if acceptIngest(t, metrics, ingestInput(base.Add(sample.offset), sample.received)) {
			t.Fatalf("sample at %v reported duplicate", sample.offset)
		}
	}
	end := base.Add(4 * time.Second)
	inclusive, err := metrics.QueryTrafficUsage(t.Context(), "node", base.Add(time.Second), end)
	if err != nil || inclusive.Received != 160 {
		t.Fatalf("inclusive totals = %+v %v, want received 160", inclusive, err)
	}
	after, err := metrics.QueryTrafficUsageAfter(t.Context(), "node", base.Add(time.Second), end)
	if err != nil || after.Received != 110 || after.Sent != 55 {
		t.Fatalf("after-boundary totals = %+v %v, want received 110 sent 55", after, err)
	}
	if empty, err := metrics.QueryTrafficUsageAfter(t.Context(), "node", end, end); err != nil || empty.Received != 0 {
		t.Fatalf("empty window = %+v %v", empty, err)
	}
}
