package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/alerter"
	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/metricserasure"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/trafficreport"
)

type backgroundConfig struct {
	ctx      context.Context
	cancel   context.CancelFunc
	alerts   *alerter.Alerter
	services *serverServices
	stores   *serverStores
	runtime  *runtimeState
	fanout   *metricsfanout.Service
}

func startBackgroundTasks(config backgroundConfig) *backgroundTasks {
	tasks := &backgroundTasks{cancel: config.cancel, alerts: config.alerts, runtime: config.runtime,
		fanout: config.fanout}
	supervisor := config.runtime.workers
	startFanoutWorker(config, tasks)
	startAlertWorkers(config, tasks)

	reportScheduler := trafficreport.NewScheduler(config.services.trafficReports)
	reportWorker := supervisor.register("traffic_reports", reportScheduler.Interval())
	reportScheduler.SetObserver(reportWorker.observe())

	applicationGc := config.services.applicationGc
	gcWorker := supervisor.register("application_gc", 15*time.Minute)
	applicationGc.SetObserver(gcWorker.observe())
	supervisor.start(&tasks.wait, gcWorker.in(config.ctx), applicationGc.Run)

	summaryScheduler := fleetsummary.NewScheduler(config.services.fleetSummaries)
	summaryWorker := supervisor.register("fleet_summaries", summaryScheduler.Interval())
	summaryScheduler.SetObserver(summaryWorker.observe())

	maintenanceScheduler := maintenance.NewScheduler(config.services.maintenance)
	maintenanceWorker := supervisor.register("maintenance", maintenanceScheduler.Interval())
	maintenanceScheduler.SetObserver(maintenanceWorker.observe())

	startOperationWorker(config, tasks)
	startBackupUploadWorker(config, tasks)
	startDiagnosticWorker(config, tasks)
	startNodeLifecycleWorkers(config, tasks)
	startAcceleratorWorker(config, tasks)
	startAgentRolloutWorker(config, tasks)
	startEnrollmentCleanupWorker(config, tasks)
	supervisor.start(&tasks.wait, reportWorker.in(config.ctx), reportScheduler.Run)
	supervisor.start(&tasks.wait, summaryWorker.in(config.ctx), summaryScheduler.Run)
	supervisor.start(&tasks.wait, maintenanceWorker.in(config.ctx), maintenanceScheduler.Run)
	if config.services.delivery != nil {
		worker := supervisor.register("notifications", time.Second)
		config.services.delivery.SetObserver(worker.observe())
		supervisor.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
			if err := config.services.delivery.Run(ctx); err != nil {
				slog.ErrorContext(ctx, "notification queue worker stopped", "reason", "queue_unavailable")
			}
		})
	}
	if config.services.metricErasure != nil {
		erasure := config.services.metricErasure
		worker := supervisor.register("metric_erasure", metricserasure.Interval)
		erasure.SetObserver(worker.observe())
		supervisor.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
			if err := erasure.Run(ctx); err != nil {
				slog.ErrorContext(ctx, "metric erasure worker stopped", "reason", "worker_error")
			}
		})
	}
	if config.services.metricRetention != nil {
		retention := config.services.metricRetention
		worker := supervisor.register("metric_retention", retention.Interval())
		retention.SetObserver(worker.observe())
		supervisor.start(&tasks.wait, worker.in(config.ctx), retention.Run)
	}
	return tasks
}

// stop先等待共享生产器及被劫持连接，再取消并等待后台任务；调用方随后关闭存储。
func (tasks *backgroundTasks) stop() {
	tasks.once.Do(func() {
		tasks.runtime.beginShutdown()
		if tasks.fanout != nil {
			_ = tasks.fanout.Close()
		}
		tasks.cancel()
		if tasks.alerts != nil {
			tasks.alerts.Stop()
		}
		tasks.wait.Wait()
	})
}

func (services *serverServices) wait() {
	services.maintenance.Wait()
	if services.applicationGc != nil {
		services.applicationGc.Wait()
	}
	services.backups.Wait()
	services.closeCommercial()
	if services.delivery != nil {
		services.delivery.Close()
	}
}

func (stores *serverStores) close() error {
	if stores.secrets != nil {
		stores.secrets.Close()
	}
	if err := stores.sqlite.Close(); err != nil {
		return fmt.Errorf("close sqlite store: %w", err)
	}
	if err := stores.mts.Close(); err != nil {
		return fmt.Errorf("close mts store: %w", err)
	}
	return nil
}

func (stores *serverStores) closeIgnoringErrors() {
	if stores.secrets != nil {
		stores.secrets.Close()
	}
	_ = stores.mts.Close()
	_ = stores.sqlite.Close()
}

func startFanoutWorker(config backgroundConfig, tasks *backgroundTasks) {
	if config.fanout == nil {
		return
	}
	worker := config.runtime.workers.register("metrics_fanout", metricsfanout.Cadence)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		if err := config.fanout.Run(ctx); err != nil {
			worker.observe()(err)
			if errors.Is(err, metricsfanout.ErrBuilderPanic) || errors.Is(err, metricsfanout.ErrObserverPanic) {
				worker.panicked.Store(true)
			}
		}
	})
}
