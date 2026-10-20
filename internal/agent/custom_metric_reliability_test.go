package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestCustomMetricRejectsNonFiniteValues(t *testing.T) {
	for _, output := range []string{"NaN", "Inf", "+Inf", "-Inf", "1e999"} {
		if _, err := parseMetricValue(output); err == nil {
			t.Errorf("accepted non-finite value %q", output)
		}
	}
}

func TestCustomMetricRejectsExcessiveCombinedOutput(t *testing.T) {
	for _, command := range []string{
		"printf '42\n'; head -c 131072 /dev/zero",
		"head -c 131072 /dev/zero >&2; printf '42\n'",
	} {
		if _, ok := CollectMetric(t.Context(), registeredMetric{Name: "oversized", ShellCommand: command}); ok {
			t.Fatal("oversized command output was accepted as a sample")
		}
	}
}

type customHeartbeatReporter struct {
	mu            sync.Mutex
	calls         int
	marshalErrors int
}

func (reporter *customHeartbeatReporter) Report(_ context.Context, sample ReportSample) (string, error) {
	metrics, custom := sample.Metrics, sample.CustomMetrics
	if _, err := json.Marshal(struct {
		Metrics model.NodeMetrics
		Custom  map[string]float64
	}{metrics, custom}); err != nil {
		reporter.mu.Lock()
		reporter.marshalErrors++
		reporter.mu.Unlock()
		return "", err
	}
	reporter.mu.Lock()
	reporter.calls++
	reporter.mu.Unlock()
	return "", nil
}

func (reporter *customHeartbeatReporter) count() int {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return reporter.calls
}

func (reporter *customHeartbeatReporter) errors() int {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return reporter.marshalErrors
}

func TestCustomMetricScriptsDoNotDelaySystemHeartbeats(t *testing.T) {
	for _, command := range []string{"exec sleep 1", "printf NaN", "exit 1"} {
		t.Run(command, func(t *testing.T) {
			registry := NewCustomMetricRegistry()
			replaceCustomRegistry(t, registry, []model.CustomMetric{{Name: "custom", ShellCommand: command, IntervalSeconds: 5, Enabled: true}})
			reporter := &customHeartbeatReporter{}
			runner := NewRunner(RunnerOptions{Interval: 10 * time.Millisecond, Collector: &fakeCollector{metrics: model.NodeMetrics{CPU: 25}},
				CustomSampler: NewCustomMetricCollector(registry), Reporter: reporter})
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- runner.Run(ctx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("collector did not stop")
				}
			})
			deadline := time.Now().Add(200 * time.Millisecond)
			for reporter.count() < 3 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if reporter.count() < 3 {
				t.Fatal("custom script delayed normal system reports")
			}
			if reporter.errors() != 0 {
				t.Fatal("custom value caused a system report encoding failure")
			}
		})
	}
}

func TestCustomMetricCommandTimesOutAndIgnoresStderrValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	if _, _, err := runCustomMetricCommandWithTimeout(ctx, "exec sleep 30", 50*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(started) >= time.Second {
		t.Fatal("script exceeded its execution budget")
	}
	value, err := executeCustomMetric(t.Context(), registeredMetric{ShellCommand: "printf not-a-value >&2; printf 3"})
	if err != nil || value != 3 {
		t.Fatalf("stderr contaminated numeric output: %v / %v", value, err)
	}
}

func TestCustomMetricCollectorRejectsNonFiniteExecutorResult(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("nonfinite")})
	collector := NewCustomMetricCollector(registry)
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return math.NaN(), nil }
	rejected := make(chan error, 1)
	collector.onError = func(_ string, err error) { rejected <- err }
	startCustomCollector(t, collector)
	select {
	case err := <-rejected:
		if !errors.Is(err, errMetricValue) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("non-finite result was not rejected")
	}
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("non-finite result became reportable")
	}
}

func TestCustomMetricSampleNeverExecutesBlockingWork(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{{Name: "slow", ShellCommand: "exec sleep 5", IntervalSeconds: 5, Enabled: true}})
	collector := NewCustomMetricCollector(registry)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = collector.Sample(ctx)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Sample performed blocking script work")
	}
}
