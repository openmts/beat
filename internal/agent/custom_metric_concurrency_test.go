package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func receiveCustomStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case name := <-started:
		return name
	case <-time.After(time.Second):
		t.Fatal("queued custom metric did not start")
		return ""
	}
}

func TestCustomMetricConcurrencyIsBoundedAndQueuedDefinitionsProgress(t *testing.T) {
	registry := NewCustomMetricRegistry()
	metrics := make([]model.CustomMetric, 2*customMetricConcurrency+1)
	for index := range metrics {
		metrics[index] = activeCustomMetric(fmt.Sprintf("metric_%d", index))
	}
	replaceCustomRegistry(t, registry, metrics)
	collector := NewCustomMetricCollector(registry)
	started, release := make(chan string, len(metrics)), make(chan struct{})
	var active, maximum atomic.Int64
	collector.execute = func(ctx context.Context, metric registeredMetric) (float64, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); count > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, count) {
				break
			}
		}
		started <- metric.Name
		select {
		case <-release:
			return 1, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	startCustomCollector(t, collector)
	seen := make(map[string]bool)
	for range customMetricConcurrency {
		seen[receiveCustomStart(t, started)] = true
	}
	select {
	case <-started:
		t.Fatal("collector exceeded its worker limit")
	case <-time.After(2 * customMetricPollInterval):
	}
	for len(seen) < len(metrics) {
		release <- struct{}{}
		name := receiveCustomStart(t, started)
		if seen[name] {
			t.Fatal("definition ran again before queued definitions")
		}
		seen[name] = true
	}
	if maximum.Load() != customMetricConcurrency {
		t.Fatalf("maximum active commands = %d", maximum.Load())
	}
}

func TestCustomMetricChangedDefinitionRejectsLateCompletion(t *testing.T) {
	registry := NewCustomMetricRegistry()
	metric := activeCustomMetric("changed")
	replaceCustomRegistry(t, registry, []model.CustomMetric{metric})
	collector := NewCustomMetricCollector(registry)
	started, canceled, release, exited := make(chan string, 1), make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(release) })
	collector.execute = func(ctx context.Context, current registeredMetric) (float64, error) {
		if current.ShellCommand != metric.ShellCommand {
			return 2, nil
		}
		started <- current.Name
		<-ctx.Done()
		close(canceled)
		<-release
		close(exited)
		return 1, nil
	}
	startCustomCollector(t, collector)
	t.Cleanup(releaseOld)
	_ = receiveCustomStart(t, started)
	replacement := metric
	replacement.ShellCommand = "printf 2"
	replaceCustomRegistry(t, registry, []model.CustomMetric{replacement})
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("changed script was not canceled")
	}
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 1 })
	releaseOld()
	<-exited
	time.Sleep(2 * customMetricPollInterval)
	if values := collector.Sample(t.Context()); len(values) != 1 || values["changed"] != 2 {
		t.Fatalf("late completion replaced the current value: %v", values)
	}
}

func TestCustomMetricStopJoinsCanceledExecutionsAndClearsSamples(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("one"), activeCustomMetric("two")})
	collector := NewCustomMetricCollector(registry)
	started, released := make(chan string, 2), make(chan struct{})
	var canceled atomic.Int64
	collector.execute = func(ctx context.Context, metric registeredMetric) (float64, error) {
		started <- metric.Name
		<-ctx.Done()
		canceled.Add(1)
		<-released
		return 9, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	release := sync.OnceFunc(func() { close(released) })
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	t.Cleanup(func() { cancel(); release() })
	for range 2 {
		_ = receiveCustomStart(t, started)
	}
	if err := collector.Run(t.Context()); err == nil {
		t.Fatal("duplicate lifecycle was accepted")
	}
	cancel()
	waitCustomCondition(t, func() bool { return canceled.Load() == 2 })
	select {
	case <-done:
		t.Fatal("collector returned while owned commands were still running")
	default:
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not join commands")
	}
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("stopped collector retained samples")
	}
	if err := collector.Run(ctx); err != nil {
		t.Fatalf("stopped lifecycle did not reset: %v", err)
	}
}
