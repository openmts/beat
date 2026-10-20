package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	customMetricPollInterval   = 100 * time.Millisecond
	customMetricMaximumAge     = time.Minute
	customMetricConcurrency    = 4
	customMetricMinimumBackoff = 5 * time.Second
	customMetricMaximumBackoff = time.Minute
)

type customMetricSample struct {
	value       float64
	completedAt time.Time
	version     uint64
	metadata    model.CustomSampleMetadata
}

type CustomMetricCollector struct {
	registry *CustomMetricRegistry
	now      func() time.Time
	execute  func(context.Context, registeredMetric) (float64, error)
	onError  func(string, error)
	mu       sync.Mutex
	samples  map[string]customMetricSample
	running  bool
}

func NewCustomMetricCollector(registry *CustomMetricRegistry) *CustomMetricCollector {
	if registry == nil {
		registry = NewCustomMetricRegistry()
	}
	return &CustomMetricCollector{registry: registry, now: time.Now, execute: executeCustomMetric,
		samples: make(map[string]customMetricSample), onError: func(name string, err error) {
			slog.Warn("custom metric collection failed", "metric", name, "error", err)
		},
	}
}

// Sample 只消费完成值，不在系统上报路径执行脚本；每份结果最多上报一次。
func (collector *CustomMetricCollector) Sample(ctx context.Context) map[string]float64 {
	samples := collector.drainSamples(ctx)
	if samples == nil {
		return nil
	}
	values := make(map[string]float64, len(samples))
	for name, sample := range samples {
		values[name] = sample.value
	}
	return values
}

func (collector *CustomMetricCollector) drainSamples(ctx context.Context) map[string]customMetricSample {
	if ctx.Err() != nil {
		return nil
	}
	collector.registry.mu.RLock()
	defer collector.registry.mu.RUnlock()
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.now()
	values := make(map[string]customMetricSample)
	for name, sample := range collector.samples {
		delete(collector.samples, name)
		version, exists := collector.registry.metricVersions[name]
		if !exists || sample.version != version || now.Before(sample.completedAt) || now.Sub(sample.completedAt) >= customMetricMaximumAge {
			continue
		}
		values[name] = sample
	}
	return values
}

func (collector *CustomMetricCollector) Run(ctx context.Context) error {
	collector.mu.Lock()
	if collector.running {
		collector.mu.Unlock()
		return errors.New("custom metric collector is already running")
	}
	collector.running = true
	collector.mu.Unlock()
	defer collector.stopped()
	scheduler := newCustomMetricScheduler(collector)
	defer scheduler.stop()
	ticker := time.NewTicker(customMetricPollInterval)
	defer ticker.Stop()
	for {
		metrics, version, changed := collector.registry.snapshot()
		scheduler.synchronize(metrics, version)
		if ctx.Err() != nil {
			return nil
		}
		scheduler.launchDue(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
		case result := <-scheduler.results:
			scheduler.complete(result)
		case <-ticker.C:
		}
	}
}

func (collector *CustomMetricCollector) stopped() {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	clear(collector.samples)
	collector.running = false
}
