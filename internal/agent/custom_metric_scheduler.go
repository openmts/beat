package agent

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type customMetricState struct {
	metric  registeredMetric
	next    time.Time
	backoff time.Duration
}

type customMetricJob struct {
	name    string
	version uint64
}

type customMetricResult struct {
	job         customMetricJob
	value       float64
	err         error
	completedAt time.Time
	metadata    model.CustomSampleMetadata
}

type customMetricScheduler struct {
	collector  *CustomMetricCollector
	version    uint64
	configured bool
	states     map[string]*customMetricState
	order      []string
	cursor     int
	active     map[customMetricJob]context.CancelFunc
	results    chan customMetricResult
	wait       sync.WaitGroup
}

func newCustomMetricScheduler(collector *CustomMetricCollector) *customMetricScheduler {
	return &customMetricScheduler{collector: collector, states: make(map[string]*customMetricState),
		active: make(map[customMetricJob]context.CancelFunc), results: make(chan customMetricResult, customMetricConcurrency)}
}

func (scheduler *customMetricScheduler) synchronize(metrics []registeredMetric, version uint64) {
	if scheduler.configured && scheduler.version == version {
		return
	}
	scheduler.version, scheduler.configured = version, true
	previous := scheduler.states
	scheduler.states = make(map[string]*customMetricState, len(metrics))
	scheduler.order = make([]string, 0, len(metrics))
	for _, metric := range metrics {
		state, exists := previous[metric.Name]
		if !exists || state.metric.version != metric.version {
			state = &customMetricState{metric: metric}
		}
		scheduler.states[metric.Name] = state
		scheduler.order = append(scheduler.order, metric.Name)
	}
	if scheduler.cursor >= len(scheduler.order) {
		scheduler.cursor = 0
	}
	scheduler.invalidateChangedMetrics()
}

func (scheduler *customMetricScheduler) invalidateChangedMetrics() {
	for job, cancel := range scheduler.active {
		if state, exists := scheduler.states[job.name]; !exists || state.metric.version != job.version {
			cancel()
		}
	}
	scheduler.collector.mu.Lock()
	for name, sample := range scheduler.collector.samples {
		if state, exists := scheduler.states[name]; !exists || sample.version != state.metric.version {
			delete(scheduler.collector.samples, name)
		}
	}
	scheduler.collector.mu.Unlock()
}

func (scheduler *customMetricScheduler) launchDue(ctx context.Context) {
	for range len(scheduler.order) {
		if len(scheduler.active) >= customMetricConcurrency {
			return
		}
		name := scheduler.order[scheduler.cursor]
		scheduler.cursor = (scheduler.cursor + 1) % len(scheduler.order)
		state := scheduler.states[name]
		job := customMetricJob{name: name, version: state.metric.version}
		if _, running := scheduler.active[job]; running || scheduler.collector.now().Before(state.next) {
			continue
		}
		jobCtx, cancel := context.WithCancel(ctx)
		scheduler.active[job] = cancel
		scheduler.wait.Add(1)
		go scheduler.execute(jobCtx, customMetricExecution{root: ctx, job: job, metric: state.metric})
	}
}

type customMetricExecution struct {
	root   context.Context
	job    customMetricJob
	metric registeredMetric
}

func (scheduler *customMetricScheduler) execute(ctx context.Context, execution customMetricExecution) {
	defer scheduler.wait.Done()
	value, err := scheduler.collector.execute(ctx, execution.metric)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && (math.IsNaN(value) || math.IsInf(value, 0)) {
		err = errMetricValue
	}
	result := customMetricResult{job: execution.job, value: value, err: err, completedAt: scheduler.collector.now()}
	if err == nil {
		id, identityErr := uuid.NewRandom()
		if identityErr != nil {
			result.err = errors.New("custom sample identity unavailable")
		} else {
			result.metadata = model.CustomSampleMetadata{SampleID: id.String(), CapturedAt: result.completedAt.UTC(),
				CadenceMS: int64(execution.metric.IntervalSeconds) * time.Second.Milliseconds(), DefinitionVersion: execution.metric.DefinitionVersion}
		}
	}
	select {
	case scheduler.results <- result:
	case <-execution.root.Done():
	}
}

func (scheduler *customMetricScheduler) complete(result customMetricResult) {
	if cancel, ok := scheduler.active[result.job]; ok {
		cancel()
		delete(scheduler.active, result.job)
	}
	state, exists := scheduler.states[result.job.name]
	if !exists || result.job.version != state.metric.version {
		return
	}
	delay := time.Duration(state.metric.IntervalSeconds) * time.Second
	if result.err != nil {
		state.backoff = customMetricFailureBackoff(state.backoff)
		delay = max(delay, state.backoff)
	} else {
		state.backoff = 0
	}
	state.next = result.completedAt.Add(delay)
	scheduler.storeSample(result)
	if result.err != nil && !errors.Is(result.err, context.Canceled) && scheduler.collector.onError != nil {
		scheduler.collector.onError(result.job.name, result.err)
	}
}

func customMetricFailureBackoff(previous time.Duration) time.Duration {
	if previous == 0 {
		return customMetricMinimumBackoff
	}
	return min(previous*2, customMetricMaximumBackoff)
}

func (scheduler *customMetricScheduler) storeSample(result customMetricResult) {
	registry := scheduler.collector.registry
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	collector := scheduler.collector
	collector.mu.Lock()
	defer collector.mu.Unlock()
	delete(collector.samples, result.job.name)
	version, exists := registry.metricVersions[result.job.name]
	if result.err == nil && exists && result.job.version == version {
		collector.samples[result.job.name] = customMetricSample{value: result.value, completedAt: result.completedAt, version: result.job.version, metadata: result.metadata}
	}
}

func (scheduler *customMetricScheduler) stop() {
	for _, cancel := range scheduler.active {
		cancel()
	}
	scheduler.wait.Wait()
}
