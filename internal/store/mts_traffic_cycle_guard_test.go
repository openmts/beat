package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestOldestActiveCycleStartPicksEarliestNode(t *testing.T) {
	now := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	oldest, found := oldestActiveCycleStart([]model.Node{{TrafficResetDay: 1}, {TrafficResetDay: 20}}, now)
	want := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	if !found || !oldest.Equal(want) {
		t.Fatalf("oldest active cycle = %v %v, want %v", oldest, found, want)
	}
	if _, found := oldestActiveCycleStart(nil, now); found {
		t.Fatal("empty fleet reported an active cycle")
	}
}

func TestProtectedTrafficCutoffClampsAndPropagatesFailures(t *testing.T) {
	metrics := metricQueryMTS(t)
	now := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	start, _ := model.BillingPeriod(now, 1)
	earlier := start.Add(-time.Hour)
	if got, err := metrics.protectedTrafficCutoff(t.Context(), earlier, now); err != nil || !got.Equal(earlier) {
		t.Fatalf("unguarded cutoff = %v %v, want %v", got, err, earlier)
	}
	metrics.WithTrafficCycleSource(func(context.Context) ([]model.Node, error) {
		return []model.Node{{TrafficResetDay: 1}}, nil
	})
	if got, err := metrics.protectedTrafficCutoff(t.Context(), now, now); err != nil || !got.Equal(start) {
		t.Fatalf("clamped cutoff = %v %v, want %v", got, err, start)
	}
	if got, err := metrics.protectedTrafficCutoff(t.Context(), earlier, now); err != nil || !got.Equal(earlier) {
		t.Fatalf("pre-cycle cutoff = %v %v, want %v", got, err, earlier)
	}
	metrics.WithTrafficCycleSource(func(context.Context) ([]model.Node, error) {
		return nil, errors.New("nodes unavailable")
	})
	if _, err := metrics.protectedTrafficCutoff(t.Context(), now, now); err == nil {
		t.Fatal("cycle source failure was ignored")
	}
}

func TestCleanupBeforeKeepsTrafficDeltasInsideTheActiveCycle(t *testing.T) {
	metrics := metricQueryMTS(t)
	now := model.NowUTC()
	resetStart, _ := model.BillingPeriod(now, 1)
	inside := resetStart.Add(time.Second)
	outside := resetStart.Add(-time.Minute)
	if err := metrics.WriteMetric(t.Context(), "node", trafficReceivedDelta, 42, inside); err != nil {
		t.Fatal(err)
	}
	if err := metrics.WriteMetric(t.Context(), "node", trafficReceivedDelta, 7, outside); err != nil {
		t.Fatal(err)
	}
	metrics.WithTrafficCycleSource(func(context.Context) ([]model.Node, error) {
		return []model.Node{{TrafficResetDay: 1}}, nil
	})
	if err := metrics.CleanupBefore(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	totals, err := metrics.QueryTrafficUsage(t.Context(), "node", outside, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if totals.Received != 42 {
		t.Fatalf("in-cycle received traffic = %v, want 42", totals.Received)
	}
}

func TestCleanupBeforeFailsClosedWhenTheCycleGuardFails(t *testing.T) {
	metrics := metricQueryMTS(t)
	metrics.WithTrafficCycleSource(func(context.Context) ([]model.Node, error) {
		return nil, errors.New("nodes unavailable")
	})
	if err := metrics.CleanupBefore(t.Context(), model.NowUTC()); err == nil {
		t.Fatal("cleanup ignored the failed cycle guard")
	}
}
