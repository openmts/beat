package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/model"
)

type schedulerService interface {
	Overview(context.Context) (model.MaintenanceOverview, error)
	StartAutomatic() error
}

type Scheduler struct {
	service  schedulerService
	interval time.Duration
	now      func() time.Time
	observer func(error)
}

func NewScheduler(service schedulerService) *Scheduler {
	return &Scheduler{
		service: service, interval: 15 * time.Minute,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// SetObserver 注册每轮调度结果钩子；须在 Run 之前调用。
func (scheduler *Scheduler) SetObserver(observer func(error)) {
	scheduler.observer = observer
}

// Interval 返回调度周期，供监督层做滞后判定。
func (scheduler *Scheduler) Interval() time.Duration {
	return scheduler.interval
}

func (scheduler *Scheduler) Run(ctx context.Context) {
	scheduler.runOnce(ctx)
	ticker := time.NewTicker(scheduler.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scheduler.runOnce(ctx)
		}
	}
}

func (scheduler *Scheduler) runOnce(ctx context.Context) {
	overview, err := scheduler.service.Overview(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "maintenance scheduler overview failed", "error", err)
		scheduler.observe(err)
		return
	}
	if !automaticMaintenanceDue(overview, scheduler.now()) {
		scheduler.observe(nil)
		return
	}
	if err := scheduler.service.StartAutomatic(); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		slog.ErrorContext(ctx, "maintenance scheduler start failed", "error", err)
		scheduler.observe(err)
		return
	}
	scheduler.observe(nil)
}

func (scheduler *Scheduler) observe(err error) {
	if scheduler.observer != nil {
		scheduler.observer(err)
	}
}

func automaticMaintenanceDue(overview model.MaintenanceOverview, now time.Time) bool {
	if !overview.Settings.AutoCleanupEnabled || overview.Status.Running {
		return false
	}
	now = now.UTC()
	scheduled := time.Date(now.Year(), now.Month(), now.Day(),
		overview.Settings.CleanupHourUTC, 0, 0, 0, time.UTC)
	if now.Before(scheduled) {
		return false
	}
	return overview.Status.LastStartedAt == nil || overview.Status.LastStartedAt.Before(scheduled)
}
