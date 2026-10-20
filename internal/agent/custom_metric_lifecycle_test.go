package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func activeCustomMetric(name string) model.CustomMetric {
	return model.CustomMetric{Name: name, ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true}
}

func waitCustomCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("custom metric state did not settle")
	}
}

func TestCustomMetricRegistryRejectsUnboundedOrInvalidSnapshots(t *testing.T) {
	registry := NewCustomMetricRegistry()
	valid := activeCustomMetric("valid")
	replaceCustomRegistry(t, registry, []model.CustomMetric{valid})
	oversized := make([]model.CustomMetric, maxCustomMetricDefinitions+1)
	for index := range oversized {
		oversized[index] = activeCustomMetric(fmt.Sprintf("metric_%d", index))
	}
	invalid := valid
	invalid.IntervalSeconds = 0
	for _, values := range [][]model.CustomMetric{oversized, {valid, valid}, {invalid}} {
		if err := registry.Replace(values); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
		if len(registry.All()) != 1 || registry.All()[0].Name != "valid" {
			t.Fatal("invalid snapshot replaced active definitions")
		}
	}
	replaceCustomRegistry(t, registry, oversized[:maxCustomMetricDefinitions])
	if len(registry.All()) != maxCustomMetricDefinitions {
		t.Fatal("valid boundary was truncated")
	}
}

func TestCustomMetricReplacementDoesNotCancelUnchangedExecution(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("retained")})
	collector := NewCustomMetricCollector(registry)
	started, canceled, added := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	collector.execute = func(ctx context.Context, metric registeredMetric) (float64, error) {
		if metric.Name == "added" {
			added <- struct{}{}
			return 2, nil
		}
		started <- struct{}{}
		<-ctx.Done()
		canceled <- struct{}{}
		return 0, ctx.Err()
	}
	startCustomCollector(t, collector)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first execution did not start")
	}
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("retained"), activeCustomMetric("added")})
	select {
	case <-added:
	case <-time.After(time.Second):
		t.Fatal("new definition did not start")
	}
	select {
	case <-canceled:
		t.Fatal("unrelated definition change canceled a retained script")
	default:
	}
}

func TestCustomMetricRemovalDropsCachedValueImmediately(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("removed")})
	collector := NewCustomMetricCollector(registry)
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return 7, nil }
	startCustomCollector(t, collector)
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 1 })
	replaceCustomRegistry(t, registry, nil)
	if values := collector.Sample(t.Context()); len(values) != 0 {
		t.Fatal("revoked cached value was reported")
	}
}

func TestCustomMetricSchedulerPurgesRemovedCacheWithoutHeartbeat(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("removed"), activeCustomMetric("retained")})
	collector := NewCustomMetricCollector(registry)
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return 5, nil }
	startCustomCollector(t, collector)
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 2 })
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("retained")})
	waitCustomCondition(t, func() bool {
		collector.mu.Lock()
		defer collector.mu.Unlock()
		_, retained := collector.samples["retained"]
		return len(collector.samples) == 1 && retained
	})
	if values := collector.Sample(t.Context()); len(values) != 1 || values["retained"] != 5 {
		t.Fatal("scheduler retained revoked cache or dropped unchanged cache")
	}
}

func TestCustomMetricValueExpiresAndIsConsumedOnlyOnce(t *testing.T) {
	registry := NewCustomMetricRegistry()
	metric := activeCustomMetric("value")
	metric.IntervalSeconds = 86400
	replaceCustomRegistry(t, registry, []model.CustomMetric{metric})
	collector := NewCustomMetricCollector(registry)
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	collector.now = func() time.Time { return time.Unix(0, now.Load()) }
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return 7, nil }
	startCustomCollector(t, collector)
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 1 })
	now.Add(int64(customMetricMaximumAge))
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("expired sample was reported as fresh")
	}
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("expired sample was retained")
	}
}

func TestCustomMetricFailureUsesBoundedBackoff(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("failing")})
	collector := NewCustomMetricCollector(registry)
	var now, failures atomic.Int64
	now.Store(time.Now().UnixNano())
	collector.now = func() time.Time { return time.Unix(0, now.Load()) }
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return 0, errors.New("failed") }
	collector.onError = func(string, error) { failures.Add(1) }
	startCustomCollector(t, collector)
	waitCustomCondition(t, func() bool { return failures.Load() == 1 })
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("failing")})
	time.Sleep(2 * customMetricPollInterval)
	if failures.Load() != 1 {
		t.Fatal("identical snapshot reset failure retry budget")
	}
	now.Add(int64(customMetricMinimumBackoff))
	waitCustomCondition(t, func() bool { return failures.Load() == 2 })
	if got := customMetricFailureBackoff(customMetricMaximumBackoff); got != customMetricMaximumBackoff {
		t.Fatal("retry backoff grew without bound")
	}
}
