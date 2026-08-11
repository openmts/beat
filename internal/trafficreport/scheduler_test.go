package trafficreport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type schedulerRunner struct {
	calls chan struct{}
	err   error
}

func (r schedulerRunner) RunDue(context.Context) error {
	select {
	case r.calls <- struct{}{}:
	default:
	}
	return r.err
}

func TestSchedulerRunsImmediatelyAndStopsWithContext(t *testing.T) {
	runner := schedulerRunner{calls: make(chan struct{}, 3)}
	scheduler := NewScheduler(runner)
	scheduler.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	for range 2 {
		select {
		case <-runner.calls:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not run")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestSchedulerRunOnceHandlesRunnerErrors(t *testing.T) {
	runner := schedulerRunner{calls: make(chan struct{}, 1), err: errors.New("run failed")}
	scheduler := NewScheduler(runner)
	scheduler.runOnce(t.Context())
	select {
	case <-runner.calls:
	default:
		t.Fatal("runner was not called")
	}
}
