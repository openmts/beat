package maintenance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeSettingsStore struct {
	mu          sync.Mutex
	settings    model.MaintenanceSettings
	status      model.MaintenanceRunStatus
	recovered   bool
	getErr      error
	recoverErr  error
	startErr    error
	completeErr error
}

func (store *fakeSettingsStore) Get(
	context.Context,
) (model.MaintenanceSettings, model.MaintenanceRunStatus, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.settings, store.status, store.getErr
}

func (store *fakeSettingsStore) Update(
	_ context.Context,
	settings model.MaintenanceSettings,
) (model.MaintenanceSettings, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.settings = settings
	return settings, nil
}

func (store *fakeSettingsStore) MarkStarted(
	_ context.Context,
	trigger string,
	startedAt time.Time,
	cutoff time.Time,
) error {
	if store.startErr != nil {
		return store.startErr
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.status = model.MaintenanceRunStatus{
		Running: true, LastStartedAt: &startedAt, LastStatus: model.MaintenanceStatusRunning,
		LastCutoffAt: &cutoff, LastTrigger: trigger,
	}
	return nil
}

func (store *fakeSettingsStore) MarkCompleted(
	_ context.Context,
	completedAt time.Time,
	duration time.Duration,
	status string,
	message string,
	integrity string,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.completeErr != nil {
		return store.completeErr
	}
	store.status.Running = false
	store.status.LastCompletedAt = &completedAt
	store.status.LastDurationMS = duration.Milliseconds()
	store.status.LastStatus = status
	store.status.LastError = message
	store.status.SQLiteIntegrity = integrity
	return nil
}

func (store *fakeSettingsStore) RecoverInterrupted(context.Context) error {
	store.recovered = true
	return store.recoverErr
}

type fakeMTS struct {
	started chan struct{}
	release chan struct{}
	err     error
	diskErr error
	// repair* 只被按需整理用例使用，随用例单 goroutine 读写。
	repairErr      error
	repairReported bool
	repairCalls    int
}

func (mts *fakeMTS) CleanupBefore(context.Context, time.Time) error {
	if mts.started != nil {
		close(mts.started)
	}
	if mts.release != nil {
		<-mts.release
	}
	return mts.err
}

func (mts *fakeMTS) DiskUsage() (int64, error) { return 10, mts.diskErr }

func (*fakeMTS) Health() (bool, []string) { return true, nil }

func (mts *fakeMTS) RepairCompactionIfDegraded(context.Context) (bool, error) {
	mts.repairCalls++
	return mts.repairReported, mts.repairErr
}

type fakeSQLite struct {
	err     error
	diskErr error
}

type fakeAudit struct {
	cutoff time.Time
	err    error
}

func (audit *fakeAudit) CleanupAuditEventsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	audit.cutoff = cutoff
	return 1, audit.err
}

func (sqlite *fakeSQLite) Maintain(context.Context) (string, error) {
	return "ok", sqlite.err
}

func (sqlite *fakeSQLite) DiskUsage() (int64, error) { return 5, sqlite.diskErr }

func newTestService(t *testing.T, mts *fakeMTS, sqlite *fakeSQLite) (*Service, *fakeSettingsStore) {
	t.Helper()
	settings := &fakeSettingsStore{settings: model.DefaultMaintenanceSettings()}
	service, err := NewService(t.Context(), settings, sqlite, mts)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service, settings
}

func TestServiceOverviewAndSettings(t *testing.T) {
	service, settings := newTestService(t, &fakeMTS{}, &fakeSQLite{})
	if !settings.recovered {
		t.Fatal("interrupted maintenance was not recovered")
	}
	overview, err := service.Overview(t.Context())
	if err != nil || overview.Storage.TotalBytes != 15 || !overview.Storage.MTSHealthy {
		t.Fatalf("overview = %+v, err = %v", overview, err)
	}
	updated := model.DefaultMaintenanceSettings()
	updated.RetentionDays = 60
	if _, err := service.UpdateSettings(t.Context(), updated); err != nil {
		t.Fatalf("update settings: %v", err)
	}
}

func TestServiceRunningStatus(t *testing.T) {
	service, _ := newTestService(t, &fakeMTS{}, &fakeSQLite{})
	if service.Running() {
		t.Fatal("idle maintenance service reported running")
	}
	service.mu.Lock()
	service.running = true
	service.mu.Unlock()
	if !service.Running() {
		t.Fatal("active maintenance service reported idle")
	}
}

func TestServicePreventsOverlappingRuns(t *testing.T) {
	mts := &fakeMTS{started: make(chan struct{}), release: make(chan struct{})}
	service, settings := newTestService(t, mts, &fakeSQLite{})
	service.now = func() time.Time { return time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC) }
	if err := service.StartManual(); err != nil {
		t.Fatalf("start manual maintenance: %v", err)
	}
	<-mts.started
	if err := service.StartAutomatic(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second start error = %v, want already running", err)
	}
	close(mts.release)
	service.Wait()
	_, status, err := settings.Get(t.Context())
	if err != nil || status.LastStatus != model.MaintenanceStatusSuccess ||
		status.LastTrigger != model.MaintenanceTriggerManual || status.SQLiteIntegrity != "ok" {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}

func TestServiceRepairsDegradedCompaction(t *testing.T) {
	mts := &fakeMTS{repairReported: true}
	service, settings := newTestService(t, mts, &fakeSQLite{})
	repaired, err := service.RepairCompactionIfDegraded(t.Context())
	if err != nil || !repaired || mts.repairCalls != 1 {
		t.Fatalf("repair = %v, err = %v, calls = %d", repaired, err, mts.repairCalls)
	}
	if service.Running() {
		t.Fatal("按需整理结束后服务仍标记为运行中")
	}
	if _, status, getErr := settings.Get(t.Context()); getErr != nil || status.LastTrigger != "" {
		t.Fatalf("按需整理写入了维护状态行: %+v, err = %v", status, getErr)
	}
}

func TestServiceRepairSkipsWhileMaintenanceRuns(t *testing.T) {
	mts := &fakeMTS{started: make(chan struct{}), release: make(chan struct{}), repairReported: true}
	service, _ := newTestService(t, mts, &fakeSQLite{})
	if err := service.StartManual(); err != nil {
		t.Fatalf("start manual maintenance: %v", err)
	}
	<-mts.started
	if _, err := service.RepairCompactionIfDegraded(t.Context()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("repair during maintenance error = %v, want already running", err)
	}
	close(mts.release)
	service.Wait()
	if mts.repairCalls != 0 {
		t.Fatalf("repair calls during maintenance = %d, want 0", mts.repairCalls)
	}
}

func TestServiceRepairPropagatesFailure(t *testing.T) {
	mts := &fakeMTS{repairErr: errors.New("repair failed")}
	service, _ := newTestService(t, mts, &fakeSQLite{})
	repaired, err := service.RepairCompactionIfDegraded(t.Context())
	if repaired || !errors.Is(err, mts.repairErr) {
		t.Fatalf("repair = %v, err = %v, want %v", repaired, err, mts.repairErr)
	}
	if service.Running() {
		t.Fatal("失败的按需整理没有释放运行标记")
	}
}

func TestServiceRecordsMaintenanceFailure(t *testing.T) {
	service, settings := newTestService(t, &fakeMTS{err: errors.New("mts failed")},
		&fakeSQLite{err: errors.New("sqlite failed")})
	if err := service.StartAutomatic(); err != nil {
		t.Fatalf("start automatic maintenance: %v", err)
	}
	service.Wait()
	_, status, err := settings.Get(t.Context())
	if err != nil || status.LastStatus != model.MaintenanceStatusFailed ||
		status.LastError == "" || status.SQLiteIntegrity != "ok" {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}

func TestServiceCleansAuditEventsOlderThan180Days(t *testing.T) {
	settings := &fakeSettingsStore{settings: model.DefaultMaintenanceSettings()}
	audit := &fakeAudit{}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{}, WithAuditStore(audit))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	now := time.Date(2026, 7, 30, 5, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if err := service.StartManual(); err != nil {
		t.Fatalf("start maintenance: %v", err)
	}
	service.Wait()
	want := now.AddDate(0, 0, -180)
	if !audit.cutoff.Equal(want) {
		t.Fatalf("audit cutoff = %v, want %v", audit.cutoff, want)
	}
}

func TestServiceInitializationAndOverviewErrors(t *testing.T) {
	settings := &fakeSettingsStore{
		settings: model.DefaultMaintenanceSettings(), recoverErr: errors.New("recover failed"),
	}
	if _, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{}); err == nil {
		t.Fatal("NewService error = nil")
	}

	tests := []struct {
		name   string
		mts    *fakeMTS
		sqlite *fakeSQLite
	}{
		{name: "MTS usage", mts: &fakeMTS{diskErr: errors.New("disk failed")}, sqlite: &fakeSQLite{}},
		{name: "SQLite usage", mts: &fakeMTS{}, sqlite: &fakeSQLite{diskErr: errors.New("disk failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newTestService(t, test.mts, test.sqlite)
			if _, err := service.Overview(t.Context()); err == nil {
				t.Fatal("Overview error = nil")
			}
		})
	}
}

func TestServiceStartErrors(t *testing.T) {
	settings := &fakeSettingsStore{
		settings: model.DefaultMaintenanceSettings(), getErr: errors.New("get failed"),
	}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := service.StartManual(); err == nil {
		t.Fatal("start with settings error = nil")
	}

	settings.getErr = nil
	settings.startErr = errors.New("start failed")
	if err := service.StartManual(); err == nil {
		t.Fatal("start with persistence error = nil")
	}
}

func TestServiceOverviewSurfacesSettingsFailure(t *testing.T) {
	settings := &fakeSettingsStore{
		settings: model.DefaultMaintenanceSettings(), getErr: errors.New("get failed"),
	}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Overview(t.Context()); err == nil {
		t.Fatal("Overview with settings error = nil")
	}
}

func TestServiceKeepsServingWhenCompletionCannotPersist(t *testing.T) {
	settings := &fakeSettingsStore{settings: model.DefaultMaintenanceSettings()}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	settings.completeErr = errors.New("completion failed")
	for attempt := range 2 {
		if err := service.StartManual(); err != nil {
			t.Fatalf("start manual maintenance %d: %v", attempt, err)
		}
		service.Wait()
		if service.Running() {
			t.Fatalf("run %d left the service marked running", attempt)
		}
	}
}
