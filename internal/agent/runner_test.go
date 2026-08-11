package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeCollector struct {
	metrics model.NodeMetrics
	err     error
}

func (f *fakeCollector) Collect(context.Context) (model.NodeMetrics, error) {
	return f.metrics, f.err
}

type fakeReporter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeReporter) Report(context.Context, model.NodeMetrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func TestRunnerReportsImmediatelyAndStops(t *testing.T) {
	reporter := &fakeReporter{}
	runner := NewRunner(RunnerOptions{Interval: time.Hour, Collector: &fakeCollector{}, Reporter: reporter})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	for {
		reporter.mu.Lock()
		calls := reporter.calls
		reporter.mu.Unlock()
		if calls > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunnerHandlesCollectionAndReportErrors(t *testing.T) {
	errorsSeen := make(chan error, 2)
	collectorRunner := NewRunner(RunnerOptions{
		Interval: time.Hour, Collector: &fakeCollector{err: errors.New("collect")}, Reporter: &fakeReporter{},
		OnError: func(err error) { errorsSeen <- err },
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = collectorRunner.Run(ctx) }()
	if err := <-errorsSeen; err == nil {
		t.Fatal("expected collection error")
	}
	cancel()

	reporterRunner := NewRunner(RunnerOptions{
		Interval: time.Hour, Collector: &fakeCollector{}, Reporter: &fakeReporter{err: errors.New("report")},
		OnError: func(err error) { errorsSeen <- err },
	})
	ctx, cancel = context.WithCancel(context.Background())
	go func() { _ = reporterRunner.Run(ctx) }()
	if err := <-errorsSeen; err == nil {
		t.Fatal("expected report error")
	}
	cancel()
}

func TestRunnerReportsOnTicker(t *testing.T) {
	reporter := &fakeReporter{}
	runner := NewRunner(RunnerOptions{
		Interval: 5 * time.Millisecond, Collector: &fakeCollector{}, Reporter: reporter,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	reporter.mu.Lock()
	calls := reporter.calls
	reporter.mu.Unlock()
	if calls < 2 {
		t.Fatalf("calls = %d, want at least 2", calls)
	}
}
