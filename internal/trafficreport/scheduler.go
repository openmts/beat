package trafficreport

import (
	"context"
	"log/slog"
	"time"
)

const defaultSchedulerInterval = 30 * time.Second

type dueRunner interface {
	RunDue(context.Context) error
}

type Scheduler struct {
	runner   dueRunner
	interval time.Duration
}

func NewScheduler(runner dueRunner) *Scheduler {
	return &Scheduler{runner: runner, interval: defaultSchedulerInterval}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	if err := s.runner.RunDue(ctx); err != nil {
		slog.ErrorContext(ctx, "traffic report scheduler failed", "error", err)
	}
}
