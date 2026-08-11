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
	mu         sync.Mutex
	settings   model.MaintenanceSettings
	status     model.MaintenanceRunStatus
	recovered  bool
	getErr     error
	recoverErr error
	startErr   error
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
