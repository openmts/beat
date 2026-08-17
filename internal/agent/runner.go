package agent

import (
	"context"
	"fmt"
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
	Report(context.Context, model.NodeMetrics, map[string]float64) (string, error)
}

// SelfHealer repairs locally detectable drift, such as a missing authorized key.
type SelfHealer interface {
	SelfHeal(context.Context, string) error
}

type Runner struct {
	interval      time.Duration
	collector     MetricsCollector
	customSampler CustomMetricSampler
	reporter      MetricsReporter
	selfHealer    SelfHealer
	onError       func(error)
}

type RunnerOptions struct {
	Interval      time.Duration
	Collector     MetricsCollector
	CustomSampler CustomMetricSampler
	Reporter      MetricsReporter
	SelfHealer    SelfHealer
	OnError       func(error)
}

func NewRunner(options RunnerOptions) *Runner {
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	return &Runner{
		interval: options.Interval, collector: options.Collector,
		customSampler: options.CustomSampler, reporter: options.Reporter,
		selfHealer: options.SelfHealer, onError: options.OnError,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.report(ctx); err != nil && ctx.Err() == nil {
		r.onError(err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.report(ctx); err != nil && ctx.Err() == nil {
				r.onError(err)
			}
		}
	}
}

func (r *Runner) report(ctx context.Context) error {
	metrics, err := r.collector.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collect metrics: %w", err)
	}
	var custom map[string]float64
	if r.customSampler != nil {
		custom = r.customSampler.Sample(ctx)
	}
	assignedKey, err := r.reporter.Report(ctx, metrics, custom)
	if err != nil {
		return fmt.Errorf("report metrics: %w", err)
	}
	if r.selfHealer != nil && assignedKey != "" {
		if err := r.selfHealer.SelfHeal(ctx, assignedKey); err != nil {
			return fmt.Errorf("self heal authorized key: %w", err)
		}
	}
	return nil
}
