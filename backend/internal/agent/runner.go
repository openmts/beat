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

type MetricsReporter interface {
	Report(context.Context, model.NodeMetrics) error
}

type Runner struct {
	interval  time.Duration
	collector MetricsCollector
	reporter  MetricsReporter
	onError   func(error)
}

type RunnerOptions struct {
	Interval  time.Duration
	Collector MetricsCollector
	Reporter  MetricsReporter
	OnError   func(error)
}

func NewRunner(options RunnerOptions) *Runner {
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	return &Runner{
		interval: options.Interval, collector: options.Collector,
		reporter: options.Reporter, onError: options.OnError,
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
	if err := r.reporter.Report(ctx, metrics); err != nil {
		return fmt.Errorf("report metrics: %w", err)
	}
	return nil
}
