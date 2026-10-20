package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/accelerators"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/store"
)

func acceleratorRouterOption(services *serverServices) api.RouterOption {
	return func(router *api.Router) {
		if services.accelerators != nil {
			api.WithAccelerators(services.accelerators)(router)
		}
	}
}

func acceleratorMonitoring(services *serverServices) acceleratorMonitor {
	if services.accelerators == nil {
		return nil
	}
	return services.accelerators
}

func (services *serverServices) initializeAccelerators(stores *serverStores) error {
	service, err := accelerators.NewService(accelerators.Config{Inventory: store.NewAcceleratorStore(stores.sqlite.DB), Metrics: stores.mts, Catalog: store.NewMetricCatalogStore(stores.sqlite.DB)})
	if err != nil {
		return fmt.Errorf("initialize accelerators: %w", err)
	}
	services.accelerators = service
	return nil
}

func startAcceleratorWorker(config backgroundConfig, tasks *backgroundTasks) {
	service := config.services.accelerators
	if service == nil {
		return
	}
	worker := config.runtime.workers.register("accelerators", time.Second)
	service.SetObserver(func(err error) {
		if err != nil {
			err = acceleratorWorkerHealth(config.ctx, service)
		}
		worker.observe()(err)
	})
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		if err := service.Run(ctx); err != nil && ctx.Err() == nil {
			worker.observe()(err)
			slog.ErrorContext(ctx, "accelerator worker stopped", "reason", "accelerator_unavailable")
		}
	})
}

func acceleratorWorkerHealth(ctx context.Context, service *accelerators.Service) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return service.Ready(ctx)
}
