package agent

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"
)

type pendingReporterFunc func(context.Context, ReportSample) (string, error)

func (report pendingReporterFunc) Report(ctx context.Context, sample ReportSample) (string, error) {
	return report(ctx, sample)
}

type countingSystemSampler struct{ calls int }

func (sampler *countingSystemSampler) Sample(context.Context) (RawSample, error) {
	sampler.calls++
	return RawSample{NetRecv: uint64(sampler.calls * 100)}, nil
}

func TestRunnerRetriesImmutablePendingSampleBeforeAdvancingCounterBaseline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sampler := &countingSystemSampler{}
		collector := NewCollector(sampler, time.Now)
		calls := 0
		var original []byte
		var initial ReportSample
		reporter := pendingReporterFunc(func(_ context.Context, sample ReportSample) (string, error) {
			calls++
			encoded, err := json.Marshal(sample)
			if err != nil {
				t.Fatal(err)
			}
			switch calls {
			case 1:
				original, initial = encoded, sample
				return "", &ReportError{Code: "storage_unavailable", Retryable: true, RetryAfter: 3 * time.Second}
			case 2:
				if string(original) != string(encoded) || sampler.calls != 1 {
					t.Fatal("pending sample or collector baseline advanced during retry")
				}
			case 3:
				if sample.SampleID == initial.SampleID || !sample.CapturedAt.Equal(initial.CapturedAt.Add(3*time.Second)) {
					t.Fatal("catch-up collection did not capture new identity/time")
				}
				if sample.Metrics.NetRecv != 100.0/3 || sampler.calls != 2 {
					t.Fatal("counter rate did not span retry delay", sample.Metrics.NetRecv)
				}
				cancel()
			default:
				t.Fatal("extra report")
			}
			return "", nil
		})
		runner := NewRunner(RunnerOptions{Interval: time.Second, Collector: collector, Reporter: reporter})
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if calls != 3 {
			t.Fatal("pending retry never completed")
		}
	})
}

func TestRunnerCancellationWhilePendingDoesNotCollectAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		sampler := &countingSystemSampler{}
		reporter := pendingReporterFunc(func(context.Context, ReportSample) (string, error) {
			time.AfterFunc(time.Second, cancel)
			return "", &ReportError{Code: "ingestion_capacity", Retryable: true, RetryAfter: time.Minute}
		})
		runner := NewRunner(RunnerOptions{Interval: time.Second, Collector: NewCollector(sampler, time.Now), Reporter: reporter})
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if sampler.calls != 1 {
			t.Fatal("cancelled retry collected another sample")
		}
	})
}
