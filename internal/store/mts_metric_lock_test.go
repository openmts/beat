package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

func TestMTSMetricQueryCancelsWhileExclusiveOperationIsRunning(t *testing.T) {
	mts := metricQueryMTS(t)
	mts.operationMu.Lock()
	t.Cleanup(mts.operationMu.Unlock)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	from := time.Unix(1_700_000_000, 0)
	go func() {
		_, err := mts.ReadMetricQuery(ctx, metricquery.Scan{NodeID: "node", Metric: "cpu", Aggregation: "avg", From: from, To: from.Add(time.Minute), Interval: time.Minute, Buckets: 1})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("query cancellation waited for backup/retention lock")
	}
}

func TestMTSMetricQueryRechecksCancellationAndClosureBeforePublishing(t *testing.T) {
	metrics := metricQueryMTS(t)
	from := time.Unix(1_700_000_000, 0)
	plan := metricquery.Scan{From: from, To: from.Add(time.Minute), Interval: time.Minute, Buckets: 1, Aggregation: "avg"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.finishMetricQuery(ctx, metricquery.NewBuckets(plan), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("completed query ignored cancellation")
	}
	invalid := plan
	invalid.Buckets = 0
	if _, err := metrics.finishMetricQuery(t.Context(), metricquery.NewBuckets(invalid), nil); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatal("invalid grid was published")
	}
	if err := metrics.engine.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.finishMetricQuery(t.Context(), metricquery.NewBuckets(plan), nil); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("completed query hid concurrent engine closure")
	}
}
