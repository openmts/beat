package fleetsummary

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRunner struct {
	err   error
	calls chan struct{}
}

func (runner *stubRunner) RunDue(context.Context) error {
	if runner.calls != nil {
		select {
		case runner.calls <- struct{}{}:
		default:
		}
	}
	return runner.err
}

func TestSchedulerExposesIntervalAndObserver(t *testing.T) {
	scheduler := NewScheduler(&stubRunner{})
	if scheduler.Interval() != defaultSchedulerInterval {
		t.Fatalf("interval = %s", scheduler.Interval())
	}
	var observed []error
	scheduler.SetObserver(func(err error) { observed = append(observed, err) })
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || observed[0] != nil {
		t.Fatalf("observed = %v", observed)
	}
	sentinel := errors.New("summary unavailable")
	scheduler.runner = &stubRunner{err: sentinel}
	scheduler.runOnce(t.Context())
	if len(observed) != 2 || !errors.Is(observed[1], sentinel) {
		t.Fatalf("observed = %v", observed)
	}
}

func TestSchedulerRunsOnIntervalAndStopsWithContext(t *testing.T) {
	runner := &stubRunner{calls: make(chan struct{}, 4)}
	scheduler := NewScheduler(runner)
	scheduler.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { scheduler.Run(ctx); close(done) }()
	deadline := time.After(2 * time.Second)
	select {
	case <-runner.calls:
	case <-deadline:
		t.Fatal("scheduler never ran")
	}
	select {
	case <-runner.calls:
	case <-deadline:
		t.Fatal("scheduler did not tick")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestSchedulerWithoutObserverIgnoresResults(t *testing.T) {
	scheduler := NewScheduler(&stubRunner{err: errors.New("ignored")})
	scheduler.runOnce(t.Context())
}
