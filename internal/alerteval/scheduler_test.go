package alerteval

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type schedulerFixture struct {
	pairs                 []DuePair
	batchErr, errorResult error
	calls, active, peak   atomic.Int64
	entered               chan struct{}
	release               chan struct{}
}

func (f *schedulerFixture) Due(_ context.Context, q DueQuery) ([]DuePair, error) {
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	start := 0
	for start < len(f.pairs) && q.After != nil && !duePairBefore(*q.After, f.pairs[start]) {
		start++
	}
	end := min(start+q.Limit, len(f.pairs))
	return f.pairs[start:end], nil
}

func (f *schedulerFixture) Evaluate(ctx context.Context, _ DuePair) error {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for old := f.peak.Load(); n > old; old = f.peak.Load() {
		if f.peak.CompareAndSwap(old, n) {
			break
		}
	}
	f.calls.Add(1)
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.errorResult
}

func schedulerPairs(n int, at time.Time) []DuePair {
	pairs := make([]DuePair, n)
	for i := range pairs {
		pairs[i] = DuePair{RuleID: "rule", NodeID: fmt.Sprintf("node-%05d", i), DueAt: at}
	}
	return pairs
}

func TestSchedulerProcessesTenThousandPairsInConsecutiveBatches(t *testing.T) {
	at := time.Now()
	fixture := &schedulerFixture{pairs: schedulerPairs(MaximumPairs, at.Add(-3*EvaluationInterval))}
	scheduler, err := NewScheduler(fixture, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scheduler.Round(t.Context(), at)
	if err != nil || result.Completed != MaximumPairs || result.Admitted != MaximumPairs || !result.Degraded {
		t.Fatal(result, err)
	}
	if fixture.peak.Load() > DefaultWorkers {
		t.Fatal("worker bound exceeded")
	}
}

func TestSchedulerRejectsOverlapAndHonorsCancellation(t *testing.T) {
	fixture := &schedulerFixture{pairs: schedulerPairs(BatchSize, time.Now()), entered: make(chan struct{}, 1), release: make(chan struct{})}
	scheduler, err := NewScheduler(fixture, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := scheduler.Round(ctx, time.Now()); done <- err }()
	<-fixture.entered
	if _, err := scheduler.Round(t.Context(), time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSchedulerFailuresAndValidation(t *testing.T) {
	for _, workers := range []int{-1, MaximumWorkers + 1} {
		if _, err := NewScheduler(&schedulerFixture{}, workers); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal(err)
		}
	}
	if _, err := NewScheduler(nil, 1); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal(err)
	}
	for _, failure := range []error{ErrConflict, ErrUnavailable} {
		fixture := &schedulerFixture{pairs: schedulerPairs(1, time.Now()), errorResult: failure}
		scheduler, err := NewScheduler(fixture, 1)
		if err != nil {
			t.Fatal(err)
		}
		result, err := scheduler.Round(t.Context(), time.Now())
		expected := int64(1)
		if errors.Is(failure, ErrConflict) {
			expected = 0
		}
		if err != nil || result.Failed != expected || result.Completed != 0 {
			t.Fatal(result, err)
		}
	}
	fixture := &schedulerFixture{batchErr: ErrUnavailable}
	scheduler, err := NewScheduler(fixture, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Round(t.Context(), time.Now()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	fixture.batchErr = nil
	fixture.pairs = []DuePair{{}}
	if _, err := scheduler.Round(t.Context(), time.Now()); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal(err)
	}
}

func TestDueBatchValidation(t *testing.T) {
	at := time.Now()
	pairs := schedulerPairs(2, at)
	query := DueQuery{At: at, Limit: 1}
	if err := validateDueBatch(pairs, query); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	query.Limit = 2
	query.After = &pairs[0]
	if err := validateDueBatch(pairs, query); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal(err)
	}
	query.After = nil
	pairs[0].DueAt = at.Add(time.Second)
	if err := validateDueBatch(pairs, query); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal(err)
	}
	if !duePairBefore(DuePair{RuleID: "a", DueAt: at}, DuePair{RuleID: "b", DueAt: at}) {
		t.Fatal("rule ordering")
	}
}
