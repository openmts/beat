package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/store"
)

const operationMaintenanceInterval = 5 * time.Second

const contactMaintenanceInterval = 30 * time.Second

// enrollmentCleanupInterval 控制过期入网挑战、票据与隐藏保留节点的清理频率。
const enrollmentCleanupInterval = time.Minute

func startAlertWorkers(config backgroundConfig, tasks *backgroundTasks) {
	supervisor := config.runtime.workers
	if config.services.evaluation == nil {
		worker := supervisor.register("alerts", config.alerts.EvaluationInterval())
		config.alerts.SetObserver(worker.observe())
		supervisor.start(&tasks.wait, worker.in(config.ctx), config.alerts.Start)
		return
	}
	worker := supervisor.register("alerts", alerteval.EvaluationInterval)
	supervisor.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		config.services.evaluation.Run(ctx, func(result alerteval.RoundResult, err error) {
			config.runtime.recordEvaluation(result)
			if (result.Degraded || result.Failed > 0) && err == nil {
				err = errors.New("alert evaluation capacity degraded")
			}
			worker.observe()(err)
			if err != nil {
				slog.WarnContext(ctx, "alert evaluation round degraded", "admitted", result.Admitted, "completed", result.Completed, "failed", result.Failed, "deferred", result.Deferred, "backlog_complete", result.BacklogComplete)
			}
		})
	})
	contact := supervisor.register("node_contacts", contactMaintenanceInterval)
	supervisor.start(&tasks.wait, contact.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{interval: contactMaintenanceInterval, run: config.alerts.MaintainContacts, observe: contact.observe()})
	})
}

// backupUploadMaintenanceInterval 控制分块上传的过期与暂存清理频率。
const backupUploadMaintenanceInterval = time.Minute

func startBackupUploadWorker(config backgroundConfig, tasks *backgroundTasks) {
	if config.services.backups == nil {
		return
	}
	worker := config.runtime.workers.register("backup_uploads", backupUploadMaintenanceInterval)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{interval: backupUploadMaintenanceInterval,
			run: config.services.backups.MaintainUploadSessions, observe: worker.observe()})
	})
}

func startOperationWorker(config backgroundConfig, tasks *backgroundTasks) {
	if config.services.operations == nil {
		return
	}
	worker := config.runtime.workers.register("remote_operations", operationMaintenanceInterval)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{interval: operationMaintenanceInterval, run: config.services.operations.Recover, observe: worker.observe()})
	})
}

type commercialPeriod struct {
	interval time.Duration
	run      func(context.Context) error
	observe  func(error)
}

func runCommercialPeriodic(ctx context.Context, period commercialPeriod) {
	ticker := time.NewTicker(period.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		period.observe(period.run(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startEnrollmentCleanupWorker 周期终结过期入网状态；非空队列只影响入网，不影响存活。
func startEnrollmentCleanupWorker(config backgroundConfig, tasks *backgroundTasks) {
	if config.stores == nil || config.stores.sqlite == nil {
		return
	}
	enrollment := store.NewAgentEnrollmentStore(config.stores.sqlite.DB)
	worker := config.runtime.workers.register("agent_enrollment", enrollmentCleanupInterval)
	observe := worker.observe()
	if registry := config.runtime.metrics.Load(); registry != nil {
		observe = combineCleanupObserver(observe, registry)
	}
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{
			interval: enrollmentCleanupInterval,
			run: func(ctx context.Context) error {
				_, err := enrollment.CleanupEnrollmentState(ctx, time.Now().UTC())
				return err
			},
			observe: observe,
		})
	})
}
