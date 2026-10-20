package applicationgc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeStore struct {
	mu          sync.Mutex
	settings    model.ApplicationRetentionSettings
	status      model.ApplicationRetentionStatus
	batches     map[string][]int64
	calls       map[string]int
	deleteErr   error
	completed   []model.ApplicationRetentionCounts
	completedAt []string
	markedStart int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		settings: model.DefaultApplicationRetentionSettings(),
		batches:  map[string][]int64{},
		calls:    map[string]int{},
	}
}

func (f *fakeStore) Get(context.Context) (model.ApplicationRetentionSettings, model.ApplicationRetentionStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, f.status, nil
}

func (f *fakeStore) Update(
	_ context.Context,
	settings model.ApplicationRetentionSettings,
) (model.ApplicationRetentionSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = settings
	return settings, nil
}

func (f *fakeStore) MarkStarted(_ context.Context, _ string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markedStart++
	f.status.Running = true
	f.status.LastStartedAt = &at
	return nil
}

func (f *fakeStore) MarkCompleted(
	_ context.Context,
	at time.Time,
	_ time.Duration,
	result string,
	code string,
	counts model.ApplicationRetentionCounts,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.Running = false
	f.status.LastStatus = result
	f.status.LastErrorCode = code
	f.completed = append(f.completed, counts)
	f.completedAt = append(f.completedAt, at.Format(time.RFC3339))
	return nil
}

func (f *fakeStore) delete(category string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[category]++
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	batches := f.batches[category]
	if len(batches) == 0 {
		return 0, nil
	}
	next := batches[0]
	f.batches[category] = batches[1:]
	return next, nil
}

func (f *fakeStore) DeleteResolvedAlertEventsBefore(context.Context, time.Time, int) (int64, error) {
	return f.delete("alerts")
}

func (f *fakeStore) DeleteAuditEventsBefore(context.Context, time.Time, int) (int64, error) {
	return f.delete("audits")
}

func (f *fakeStore) DeleteRevokedSessionsBefore(context.Context, time.Time, time.Time, int) (int64, error) {
	return f.delete("sessions")
}

func TestDeleteAllRepeatsBoundedBatchesUntilDrained(t *testing.T) {
	store := newFakeStore()
	store.batches["sessions"] = []int64{model.ApplicationRetentionBatchSize, 2}
	store.batches["alerts"] = []int64{model.ApplicationRetentionBatchSize, 0}
	store.batches["audits"] = []int64{1}
	service := New(t.Context(), store)
	counts, err := service.pass(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if counts.Sessions != model.ApplicationRetentionBatchSize+2 || counts.AlertEvents != model.ApplicationRetentionBatchSize ||
		counts.AuditEvents != 1 {
		t.Fatalf("counts = %#v", counts)
	}
	if store.calls["sessions"] != 2 || store.calls["alerts"] != 2 || store.calls["audits"] != 1 {
		t.Fatalf("calls = %#v", store.calls)
	}
}

func TestPassReportsCategoryFailuresAndKeepsOtherCleanup(t *testing.T) {
	store := newFakeStore()
	store.deleteErr = errors.New("database unavailable")
	service := New(t.Context(), store)
	if _, err := service.pass(t.Context(), time.Now().UTC()); err == nil {
		t.Fatal("delete failure must surface")
	}
	if store.calls["sessions"] != 1 || store.calls["alerts"] != 1 || store.calls["audits"] != 1 {
		t.Fatalf("calls = %#v", store.calls)
	}
}

func TestManualTriggerRunsOnceAndRecordsCompletion(t *testing.T) {
	store := newFakeStore()
	service := New(t.Context(), store)
	service.WithClock(func() time.Time { return time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC) })
	if err := service.TriggerManual(); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if err := service.TriggerManual(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second trigger = %v", err)
	}
	service.Wait()
	if store.markedStart != 1 || len(store.completed) != 1 {
		t.Fatalf("marked = %d, completed = %#v", store.markedStart, store.completed)
	}
	if store.status.LastStatus != model.ApplicationRetentionStatusSuccess || store.status.Running {
		t.Fatalf("status = %#v", store.status)
	}
	if err := service.TriggerManual(); err != nil {
		t.Fatalf("trigger after completion: %v", err)
	}
	service.Wait()
	if store.markedStart != 2 {
		t.Fatalf("marked after second trigger = %d", store.markedStart)
	}
}

func TestExecuteRecordsFailureCategoryWithoutSecrets(t *testing.T) {
	store := newFakeStore()
	store.deleteErr = context.DeadlineExceeded
	service := New(t.Context(), store)
	if err := service.TriggerManual(); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	service.Wait()
	if store.status.LastStatus != model.ApplicationRetentionStatusFailed || store.status.LastErrorCode != "timeout" {
		t.Fatalf("status = %#v", store.status)
	}
}

func TestAutomaticCleanupDueSemantics(t *testing.T) {
	now := time.Date(2026, 10, 15, 4, 30, 0, 0, time.UTC)
	settings := model.DefaultApplicationRetentionSettings()
	settings.CleanupHourUTC = 3
	earlier := now.Add(-24 * time.Hour)
	scheduled := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		change func(*model.ApplicationRetentionOverview)
		want   bool
	}{
		{"due after scheduled hour", func(o *model.ApplicationRetentionOverview) {}, true},
		{"already run today", func(o *model.ApplicationRetentionOverview) { o.Status.LastStartedAt = &scheduled }, false},
		{"ran yesterday", func(o *model.ApplicationRetentionOverview) { o.Status.LastStartedAt = &earlier }, true},
		{"disabled", func(o *model.ApplicationRetentionOverview) { o.Settings.AutoCleanupEnabled = false }, false},
		{"already running", func(o *model.ApplicationRetentionOverview) { o.Status.Running = true }, false},
		{"future hour", func(o *model.ApplicationRetentionOverview) { o.Settings.CleanupHourUTC = 23 }, false},
	}
	for _, test := range cases {
		overview := model.ApplicationRetentionOverview{Settings: settings}
		test.change(&overview)
		if got := automaticCleanupDue(overview, now); got != test.want {
			t.Fatalf("%s = %v, want %v", test.name, got, test.want)
		}
	}
	if automaticCleanupDue(model.ApplicationRetentionOverview{Settings: settings},
		time.Date(2026, 10, 15, 2, 30, 0, 0, time.UTC)) {
		t.Fatal("before scheduled hour must not be due")
	}
}

func TestOverviewEchoesFixedSessionPolicy(t *testing.T) {
	store := newFakeStore()
	service := New(t.Context(), store)
	overview, err := service.Overview(t.Context())
	if err != nil || overview.SessionRetentionDays != model.ApplicationSessionRetentionDays {
		t.Fatalf("overview = %#v, error = %v", overview, err)
	}
	if _, err := service.UpdateSettings(t.Context(), model.ApplicationRetentionSettings{
		AlertEventDays: 30, AuditEventDays: 30, CleanupHourUTC: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
}
