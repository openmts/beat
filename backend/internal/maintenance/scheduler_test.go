package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeSchedulerService struct {
	overview model.MaintenanceOverview
	starts   int
}

func (service *fakeSchedulerService) Overview(context.Context) (model.MaintenanceOverview, error) {
	return service.overview, nil
}

func (service *fakeSchedulerService) StartAutomatic() error {
	service.starts++
	return nil
}

func TestAutomaticMaintenanceDue(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	overview := model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()}
	if !automaticMaintenanceDue(overview, now) {
		t.Fatal("maintenance should be due after the configured hour")
	}
	started := time.Date(2026, 7, 30, 3, 30, 0, 0, time.UTC)
	overview.Status.LastStartedAt = &started
	if automaticMaintenanceDue(overview, now) {
		t.Fatal("maintenance should run only once per scheduled day")
	}
	overview.Status.LastStartedAt = nil
	overview.Settings.AutoCleanupEnabled = false
	if automaticMaintenanceDue(overview, now) {
		t.Fatal("disabled maintenance should not be due")
	}
}

func TestSchedulerStartsDueMaintenanceAndStops(t *testing.T) {
	service := &fakeSchedulerService{overview: model.MaintenanceOverview{
		Settings: model.DefaultMaintenanceSettings(),
	}}
	scheduler := NewScheduler(service)
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	scheduler.runOnce(t.Context())
	if service.starts != 1 {
		t.Fatalf("automatic starts = %d, want 1", service.starts)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
