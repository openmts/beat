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
	observer func(error)
}

func NewScheduler(runner dueRunner) *Scheduler {
	return &Scheduler{runner: runner, interval: defaultSchedulerInterval}
}

// SetObserver 注册每轮调度结果钩子；须在 Run 之前调用。
func (s *Scheduler) SetObserver(observer func(error)) {
	s.observer = observer
}

// Interval 返回调度周期，供监督层做滞后判定。
func (s *Scheduler) Interval() time.Duration {
	return s.interval
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
	err := s.runner.RunDue(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "traffic report scheduler failed", "error", err)
	}
	if s.observer != nil {
		s.observer(err)
	}
}
