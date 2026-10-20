package maintenance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// fakeSchedulerService 会被调度器 goroutine 与测试 goroutine 同时访问，计数必须加锁：
// 竞态检测下无锁读写 starts 会让整包门禁失败。
type fakeSchedulerService struct {
	mu             sync.Mutex
	overview       model.MaintenanceOverview
	overviewErr    error
	startErr       error
	starts         int
	repairErr      error
	repairReported bool
	repairs        int
}

// startCount 返回自动启动次数：测试在调度器运行时轮询它，因此必须与 StartAutomatic 共用同一把锁。
func (service *fakeSchedulerService) startCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.starts
}

func (service *fakeSchedulerService) Overview(context.Context) (model.MaintenanceOverview, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.overviewErr != nil {
		return model.MaintenanceOverview{}, service.overviewErr
	}
	return service.overview, nil
}

func (service *fakeSchedulerService) repairCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.repairs
}

func (service *fakeSchedulerService) RepairCompactionIfDegraded(context.Context) (bool, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.repairs++
	return service.repairReported, service.repairErr
}

func (service *fakeSchedulerService) StartAutomatic() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.startErr != nil {
		return service.startErr
	}
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

func TestSchedulerRepairsDegradedCompaction(t *testing.T) {
	service := &fakeSchedulerService{
		overview:       degradedOverview(),
		repairReported: true,
	}
	scheduler := NewScheduler(service)
	// 固定到清理小时之前：本轮只验证按需整理，不掺入当日维护。
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC) }
	observed := []error{}
	scheduler.SetObserver(func(err error) { observed = append(observed, err) })
	scheduler.runOnce(t.Context())
	if service.repairCount() != 1 {
		t.Fatalf("repairs = %d, want 1", service.repairCount())
	}
	if service.startCount() != 0 {
		t.Fatalf("整理当轮还启动了每日维护: %d", service.startCount())
	}
	if len(observed) != 1 || observed[0] != nil {
		t.Fatalf("整理当轮结果 = %v", observed)
	}
}

func TestSchedulerSkipsRepairWhenStorageHealthy(t *testing.T) {
	service := &fakeSchedulerService{
		overview: model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()},
	}
	service.overview.Storage.MTSHealthy = true
	scheduler := NewScheduler(service)
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	scheduler.runOnce(t.Context())
	if service.repairCount() != 0 {
		t.Fatalf("健康存储触发了 %d 次整理", service.repairCount())
	}
	if service.startCount() != 1 {
		t.Fatalf("到达清理小时的健康存储没有启动每日维护: %d", service.startCount())
	}
}

func TestSchedulerReportsRepairFailures(t *testing.T) {
	service := &fakeSchedulerService{overview: degradedOverview(), repairErr: errors.New("repair failed")}
	scheduler := NewScheduler(service)
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	observed := []error{}
	scheduler.SetObserver(func(err error) { observed = append(observed, err) })
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || !errors.Is(observed[0], service.repairErr) {
		t.Fatalf("整理失败未上报: %v", observed)
	}
	if service.startCount() != 0 {
		t.Fatalf("整理失败后仍启动了每日维护: %d", service.startCount())
	}
}

func TestSchedulerIgnoresRepairWhileMaintenanceRuns(t *testing.T) {
	overview := degradedOverview()
	overview.Status.Running = true
	service := &fakeSchedulerService{overview: overview, repairErr: ErrAlreadyRunning}
	scheduler := NewScheduler(service)
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	observed := []error{}
	scheduler.SetObserver(func(err error) { observed = append(observed, err) })
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || observed[0] != nil {
		t.Fatalf("维护中的整理轮次被当作失败: %v", observed)
	}
	if service.startCount() != 0 {
		t.Fatalf("整理轮次还启动了每日维护: %d", service.startCount())
	}
}

func degradedOverview() model.MaintenanceOverview {
	overview := model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()}
	overview.Storage.MTSHealthy = false
	overview.Storage.MTSHealthReasons = []string{"compaction degraded on shard beat/autogen/1"}
	return overview
}

func TestSchedulerObserverHooks(t *testing.T) {
	service := &fakeSchedulerService{}
	scheduler := NewScheduler(service)
	var observed error
	scheduler.SetObserver(func(err error) { observed = err })
	if scheduler.Interval() != 15*time.Minute {
		t.Fatalf("interval = %v", scheduler.Interval())
	}
	_ = observed
}

func TestSchedulerReportsCycleOutcomes(t *testing.T) {
	service := &fakeSchedulerService{overviewErr: errors.New("overview failed")}
	scheduler := NewScheduler(service)
	observed := []error{}
	scheduler.SetObserver(func(err error) { observed = append(observed, err) })
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || !errors.Is(observed[0], service.overviewErr) {
		t.Fatalf("overview failure was not reported: %v", observed)
	}

	service.overviewErr = nil
	service.overview = model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()}
	service.startErr = errors.New("start failed")
	observed = nil
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || !errors.Is(observed[0], service.startErr) {
		t.Fatalf("start failure was not reported: %v", observed)
	}

	service.startErr = ErrAlreadyRunning
	observed = nil
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || observed[0] != nil {
		t.Fatalf("already running cycle reported an error: %v", observed)
	}

	service.overview.Settings.CleanupHourUTC = 23
	observed = nil
	scheduler.runOnce(t.Context())
	if len(observed) != 1 || observed[0] != nil || service.starts != 0 {
		t.Fatalf("not-due cycle started maintenance: %v starts=%d", observed, service.starts)
	}
}

func TestAutomaticMaintenanceWaitsForScheduledHour(t *testing.T) {
	now := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	overview := model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()}
	overview.Settings.CleanupHourUTC = 3
	if automaticMaintenanceDue(overview, now) {
		t.Fatal("maintenance ran before the configured hour")
	}
}

func TestSchedulerRunsRepeatedCycles(t *testing.T) {
	service := &fakeSchedulerService{overview: model.MaintenanceOverview{Settings: model.DefaultMaintenanceSettings()}}
	scheduler := NewScheduler(service)
	scheduler.interval = time.Millisecond
	// 固定到「清理小时之后」的时钟：默认 3 点 UTC 之前调度器本来就不该启动，
	// 用真实时间会让这个用例在每天 00:00–03:00 UTC 之间必然失败。
	scheduler.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for service.startCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	if service.startCount() < 2 {
		t.Fatalf("scheduler ran %d cycles, want at least 2", service.startCount())
	}
}
