package main

import (
	"context"
	"log/slog"
	"time"
)

func startNodeLifecycleWorkers(config backgroundConfig, tasks *backgroundTasks) {
	if config.services.geoIP != nil {
		worker := config.runtime.workers.register("geoip", 30*time.Second)
		config.services.geoIP.SetObserver(worker.observe())
		config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
			if err := config.services.geoIP.Run(ctx); err != nil && ctx.Err() == nil {
				worker.observe()(err)
				slog.ErrorContext(ctx, "GeoIP worker stopped", "reason", "geoip_unavailable")
			}
		})
	}
	if config.services.nodeLifecycle != nil {
		worker := config.runtime.workers.register("node_lifecycle", time.Minute)
		config.services.nodeLifecycle.SetObserver(worker.observe())
		config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
			if err := config.services.nodeLifecycle.Run(ctx); err != nil && ctx.Err() == nil {
				worker.observe()(err)
				slog.ErrorContext(ctx, "node lifecycle worker stopped", "reason", "lifecycle_unavailable")
			}
		})
	}
}
