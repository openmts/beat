package agent

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

type mutablePendingCustomCache struct {
	values  map[string]float64
	samples int
}

func (cache *mutablePendingCustomCache) Sample(context.Context) map[string]float64 {
	cache.samples++
	return cache.values
}

func (cache *mutablePendingCustomCache) SampleProvenance(ctx context.Context) model.CollectedCustomSamples {
	values := cache.Sample(ctx)
	sources := make(map[string]model.CustomSampleMetadata)
	for name := range values {
		sources[name] = testutil.CustomSourceFixture(time.Now(), model.CustomMetric{ID: "fixture", Name: name, ShellCommand: "printf 1", IntervalSeconds: 5})
	}
	return model.CollectedCustomSamples{Values: values, Samples: sources}
}

func TestCustomCacheUpdateAndRevocationCannotMutatePendingReport(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "updated", true: "revoked"}[revoke], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cache := &mutablePendingCustomCache{values: map[string]float64{"queue": 7}}
				calls := 0
				var first ReportSample
				reporter := pendingReporterFunc(func(_ context.Context, sample ReportSample) (string, error) {
					calls++
					switch calls {
					case 1:
						first = sample
						if revoke {
							delete(cache.values, "queue")
						} else {
							cache.values["queue"] = 9
						}
						return "", &ReportError{Retryable: true, RetryAfter: time.Second}
					case 2:
						if sample.SampleID != first.SampleID || !sample.CapturedAt.Equal(first.CapturedAt) || sample.CustomMetrics["queue"] != 7 || cache.samples != 1 {
							t.Fatal("background cache changed immutable pending sample")
						}
					case 3:
						value, exists := sample.CustomMetrics["queue"]
						if sample.SampleID == first.SampleID || cache.samples != 2 || revoke && exists || !revoke && value != 9 {
							t.Fatal("ACK did not permit latest custom cache")
						}
						cancel()
					}
					return "", nil
				})
				runner := NewRunner(RunnerOptions{Interval: time.Second, Collector: &fakeCollector{}, CustomSampler: cache, Reporter: reporter})
				if err := runner.Run(ctx); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
