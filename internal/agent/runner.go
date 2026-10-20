package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/beat/backend/internal/model"
)

type MetricsCollector interface {
	Collect(context.Context) (model.NodeMetrics, error)
}

type CustomMetricSampler interface {
	Sample(context.Context) map[string]float64
}

type MetricsReporter interface {
	Report(context.Context, ReportSample) (string, error)
}

type SelfHealer interface {
	SelfHeal(context.Context, string) error
}

type Runner struct {
	accelerators    AcceleratorSampler
	interval        time.Duration
	collector       MetricsCollector
	customSampler   CustomMetricSampler
	reporter        MetricsReporter
	selfHealer      SelfHealer
	onError         func(error)
	reportNotBefore time.Time
}

type RunnerOptions struct {
	Accelerators  AcceleratorSampler
	Interval      time.Duration
	Collector     MetricsCollector
	CustomSampler CustomMetricSampler
	Reporter      MetricsReporter
	SelfHealer    SelfHealer
	OnError       func(error)
}

type pendingAgentSample struct {
	sample    ReportSample
	createdAt time.Time
	retries   int
}

func NewRunner(options RunnerOptions) *Runner {
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	return &Runner{interval: options.Interval, collector: options.Collector, customSampler: options.CustomSampler, accelerators: options.Accelerators,
		reporter: options.Reporter, selfHealer: options.SelfHealer, onError: options.OnError}
}

func (r *Runner) Run(ctx context.Context) error {
	if r.interval <= 0 || r.collector == nil || r.reporter == nil {
		return errors.New("invalid metrics runner configuration")
	}
	customErrors, stopCustom := r.startCustomSampler(ctx)
	defer stopCustom()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var pending *pendingAgentSample
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-customErrors:
			customErrors = nil
			r.reportCustomSamplerError(ctx, err)
		case <-timer.C:
			delay := r.advanceReport(ctx, &pending)
			timer.Reset(delay)
		}
	}
}

func (r *Runner) advanceReport(ctx context.Context, pending **pendingAgentSample) time.Duration {
	if ctx.Err() != nil {
		return r.interval
	}
	if *pending == nil {
		if delay := time.Until(r.reportNotBefore); delay > 0 {
			return delay
		}
		collected, err := r.collectReport(ctx)
		if err != nil {
			if ctx.Err() == nil {
				r.onError(err)
			}
			return r.interval
		}
		*pending = collected
	}
	current := *pending
	remaining := time.Until(current.createdAt.Add(model.MaximumAgentSampleAge))
	if remaining <= 0 {
		r.onError(&ReportError{Code: "sample_expired"})
		*pending = nil
		return max(time.Until(r.reportNotBefore), 0)
	}
	assigned, err := r.reporter.Report(ctx, current.sample)
	if err != nil {
		return r.failedReport(ctx, pending, err)
	}
	*pending = nil
	if r.selfHealer != nil && assigned != "" {
		if err := r.selfHealer.SelfHeal(ctx, assigned); err != nil && ctx.Err() == nil {
			r.onError(fmt.Errorf("self heal authorized key: %w", err))
		}
	}
	return max(time.Until(current.createdAt.Add(r.interval)), 0)
}

func (r *Runner) collectReport(ctx context.Context) (*pendingAgentSample, error) {
	metrics, err := r.collector.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect metrics: %w", err)
	}
	custom, err := r.collectCustomSamples(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sample, err := newReportSample(metrics, custom, now)
	if err != nil {
		return nil, err
	}
	r.attachAccelerators(&sample)
	return &pendingAgentSample{sample: sample, createdAt: now}, nil
}

func (r *Runner) failedReport(ctx context.Context, pending **pendingAgentSample, err error) time.Duration {
	if ctx.Err() != nil {
		return r.interval
	}
	r.onError(fmt.Errorf("report metrics: %w", err))
	current := *pending
	var failure *ReportError
	var retryAfter time.Duration
	if errors.As(err, &failure) {
		if !failure.Retryable {
			*pending = nil
			return max(time.Until(current.createdAt.Add(r.interval)), 0)
		}
		retryAfter = failure.RetryAfter
	}
	if retryAfter > 0 {
		r.reportNotBefore = time.Now().Add(min(retryAfter, model.MaximumAgentSampleAge))
	}
	delay := reportRetryDelay(current.retries, rand.Float64(), retryAfter)
	current.retries++
	return min(delay, max(time.Until(current.createdAt.Add(model.MaximumAgentSampleAge)), 0))
}
