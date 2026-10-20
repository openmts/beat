package notificationqueue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func (service *Service) Run(ctx context.Context) error {
	service.mu.Lock()
	if service.running || service.sender == nil {
		service.mu.Unlock()
		return ErrQueueUnavailable
	}
	runCtx, cancel := context.WithCancel(ctx)
	service.cancel, service.running = cancel, true
	service.wg.Add(1)
	service.mu.Unlock()
	defer service.finishRun(cancel)
	service.broken.Store(false)
	err := service.runWorkers(runCtx, cancel)
	service.broken.Store(err != nil)
	return err
}

func (service *Service) finishRun(cancel context.CancelFunc) {
	cancel()
	service.mu.Lock()
	service.running = false
	service.mu.Unlock()
	service.wg.Done()
}

func (service *Service) runWorkers(ctx context.Context, cancel context.CancelFunc) error {
	errorsSeen := make(chan error, service.workers+1)
	var workers sync.WaitGroup
	start := func(task func(context.Context) error) {
		workers.Go(func() {
			if err := task(ctx); err != nil && ctx.Err() == nil {
				errorsSeen <- err
				cancel()
			}
		})
	}
	start(service.maintenanceWorker)
	for index := range service.workers {
		start(func(ctx context.Context) error { return service.work(ctx, fmt.Sprintf("worker-%d", index)) })
	}
	workers.Wait()
	select {
	case err := <-errorsSeen:
		return err
	default:
		return nil
	}
}

func (service *Service) maintenanceWorker(ctx context.Context) error {
	timer := time.NewTicker(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := service.maintain(ctx); err != nil {
				return err
			}
		}
	}
}
