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
	RepairCompactionIfDegraded(context.Context) (bool, error)
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
	if err := scheduler.repairDegradedCompaction(ctx, overview); err != nil {
		slog.ErrorContext(ctx, "maintenance compaction repair failed", "error", err)
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

// repairDegradedCompaction 在存储健康原因报出分片压缩退化时立即整理一次，
// 免得就绪探针一直等到下一次每日维护或重启；已有一轮维护在跑时不动。
func (scheduler *Scheduler) repairDegradedCompaction(
	ctx context.Context,
	overview model.MaintenanceOverview,
) error {
	if overview.Storage.MTSHealthy {
		return nil
	}
	repaired, err := scheduler.service.RepairCompactionIfDegraded(ctx)
	switch {
	case errors.Is(err, ErrAlreadyRunning):
		return nil
	case err != nil:
		return err
	case repaired:
		slog.WarnContext(ctx, "repaired degraded MTS compaction", "reasons", overview.Storage.MTSHealthReasons)
	}
	return nil
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
