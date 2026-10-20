package metricquery

import (
	"testing"
	"time"
)

func TestPlanBucketsDerivesDeterministicWidths(t *testing.T) {
	from := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		name     string
		span     time.Duration
		wantInt  time.Duration
		wantLen  int
		wantPlan bool
	}{
		{name: "one minute", span: time.Minute, wantInt: time.Second, wantLen: 60, wantPlan: true},
		{name: "one hour", span: time.Hour, wantInt: 6 * time.Second, wantLen: 600, wantPlan: true},
		{name: "thirty days", span: 30 * 24 * time.Hour, wantInt: 4320 * time.Second, wantLen: 600, wantPlan: true},
		{name: "thirty one days", span: 31 * 24 * time.Hour, wantInt: 4464 * time.Second, wantLen: 600, wantPlan: true},
		{name: "sub second", span: time.Millisecond, wantInt: time.Second, wantLen: 1, wantPlan: true},
		{name: "zero span", span: 0},
		{name: "negative span", span: -time.Minute},
		{name: "beyond maximum range", span: 32 * 24 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interval, buckets, ok := PlanBuckets(from, from.Add(test.span), MaximumPoints)
			if ok != test.wantPlan {
				t.Fatalf("ok = %v, want %v", ok, test.wantPlan)
			}
			if !ok {
				return
			}
			if interval != test.wantInt || buckets != test.wantLen {
				t.Fatalf("plan = %v/%d, want %v/%d", interval, buckets, test.wantInt, test.wantLen)
			}
			if buckets > MaximumPoints {
				t.Fatalf("bucket count %d exceeds the point budget", buckets)
			}
		})
	}
	if _, _, ok := PlanBuckets(from, from.Add(time.Hour), MinimumPoints-1); ok {
		t.Fatal("plan accepted a point budget below the minimum")
	}
}
