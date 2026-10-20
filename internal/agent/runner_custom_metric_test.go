package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type failedCustomSampler struct{ err error }

func (*failedCustomSampler) Sample(context.Context) map[string]float64 { return nil }

func (sampler *failedCustomSampler) Run(context.Context) error { return sampler.err }

func TestRunnerReportsBackgroundCustomMetricFailureAndContinuesHeartbeats(t *testing.T) {
	for _, samplerError := range []error{errors.New("sampler stopped"), nil} {
		reporter := &customHeartbeatReporter{}
		reported := make(chan error, 1)
		runner := NewRunner(RunnerOptions{Interval: time.Millisecond, Collector: &fakeCollector{}, Reporter: reporter,
			CustomSampler: &failedCustomSampler{err: samplerError}, OnError: func(err error) { reported <- err }})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		waitCustomCondition(t, func() bool { return reporter.count() >= 3 })
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("runner did not stop")
		}
		if samplerError != nil {
			select {
			case err := <-reported:
				if !errors.Is(err, samplerError) || !strings.Contains(err.Error(), "custom metric collector") {
					t.Fatal(err)
				}
			default:
				t.Fatal("background failure was ignored")
			}
		} else {
			select {
			case err := <-reported:
				t.Fatalf("clean exit reported as an error: %v", err)
			default:
			}
		}
	}
}
