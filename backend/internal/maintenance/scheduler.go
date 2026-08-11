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
}

func NewScheduler(service schedulerService) *Scheduler {
	return &Scheduler{
		service: service, interval: 15 * time.Minute,
		now: func() time.Time { return time.Now().UTC() },
	}
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
		return
	}
	now := scheduler.now()
	if !automaticMaintenanceDue(overview, now) {
		return
	}
	if err := scheduler.service.StartAutomatic(); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		slog.ErrorContext(ctx, "maintenance scheduler start failed", "error", err)
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
