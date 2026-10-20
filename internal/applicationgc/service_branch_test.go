package applicationgc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// stubStore 允许逐个注入失败，用于覆盖调度与收尾的错误分支。
type stubStore struct {
	settings    model.ApplicationRetentionSettings
	status      model.ApplicationRetentionStatus
	getErr      error
	startErr    error
	completeErr error
	deleteErr   error
	batches     []int64
	completed   []model.ApplicationRetentionCounts
	codes       []string
}

func newStubStore() *stubStore {
	return &stubStore{settings: model.DefaultApplicationRetentionSettings()}
}

func (store *stubStore) Get(context.Context) (model.ApplicationRetentionSettings, model.ApplicationRetentionStatus, error) {
	return store.settings, store.status, store.getErr
}

func (store *stubStore) Update(
	_ context.Context,
	settings model.ApplicationRetentionSettings,
) (model.ApplicationRetentionSettings, error) {
	store.settings = settings
	return settings, nil
}

func (store *stubStore) MarkStarted(context.Context, string, time.Time) error { return store.startErr }

func (store *stubStore) MarkCompleted(
	_ context.Context,
	_ time.Time,
	_ time.Duration,
	_ string,
	code string,
	counts model.ApplicationRetentionCounts,
) error {
	store.completed = append(store.completed, counts)
	store.codes = append(store.codes, code)
	return store.completeErr
}

func (store *stubStore) next() (int64, error) {
	if store.deleteErr != nil {
		return 0, store.deleteErr
	}
	if len(store.batches) == 0 {
		return 0, nil
	}
	next := store.batches[0]
	store.batches = store.batches[1:]
	return next, nil
}

func (store *stubStore) DeleteResolvedAlertEventsBefore(context.Context, time.Time, int) (int64, error) {
	return store.next()
}

func (store *stubStore) DeleteAuditEventsBefore(context.Context, time.Time, int) (int64, error) {
	return store.next()
}

func (store *stubStore) DeleteRevokedSessionsBefore(context.Context, time.Time, time.Time, int) (int64, error) {
	return store.next()
}

func TestServiceGuardsAndObserver(t *testing.T) {
	var nilService *Service
	nilService.SetObserver(func(error) { t.Fatal("nil receiver invoked observer") })
	service := New(t.Context(), newStubStore())
	if service.Running() {
		t.Fatal("idle service reported running")
	}
	service.Wait()
	service.observe(errors.New("ignored"))
	var observed error
	service.SetObserver(func(err error) { observed = err })
	sentinel := errors.New("observed")
	service.observe(sentinel)
	if !errors.Is(observed, sentinel) {
		t.Fatalf("observed = %v", observed)
	}
}

func TestTriggerManualRejectsConcurrentRun(t *testing.T) {
	service := New(t.Context(), newStubStore())
	service.live = true
	if err := service.TriggerManual(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("trigger = %v", err)
	}
}

func TestRunReportsAndStopsOnCancellation(t *testing.T) {
	store := newStubStore()
	store.getErr = errors.New("overview unavailable")
	service := New(context.WithoutCancel(t.Context()), store)
	// 观察者由服务自身的 goroutine 调用，这里用带缓冲的通道交接，避免与测试 goroutine 竞争。
	observed := make(chan error, 8)
	service.SetObserver(func(err error) { observed <- err })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { service.Run(ctx); close(done) }()
	select {
	case first := <-observed:
		if first == nil {
			t.Fatal("observer reported nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not report a failure")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	// Run 返回后不会再有观察者调用，这里可以安全地做非阻塞排空。
	select {
	case extra := <-observed:
		t.Fatalf("unexpected extra observation: %v", extra)
	default:
	}
}

func TestRunDueSkipsAndReportsFailures(t *testing.T) {
	idle := newStubStore()
	idle.settings.AutoCleanupEnabled = false
	if err := New(t.Context(), idle).runDue(t.Context()); err != nil {
		t.Fatalf("disabled cleanup = %v", err)
	}
	busy := New(t.Context(), newStubStore())
	busy.live = true
	if err := busy.runDue(t.Context()); err != nil {
		t.Fatalf("busy scheduler = %v", err)
	}
	broken := newStubStore()
	broken.getErr = errors.New("ledger unavailable")
	if err := New(t.Context(), broken).runDue(t.Context()); err == nil {
		t.Fatal("overview failure was ignored")
	}
}

func TestExecuteReportsStartAndCompletionFailures(t *testing.T) {
	start := newStubStore()
	start.startErr = errors.New("start rejected")
	if err := New(t.Context(), start).execute(model.ApplicationRetentionTriggerManual); err == nil {
		t.Fatal("start failure was ignored")
	}
	if len(start.completed) != 0 {
		t.Fatalf("completion recorded after start failure: %v", start.completed)
	}
	complete := newStubStore()
	complete.completeErr = errors.New("completion rejected")
	if err := New(t.Context(), complete).execute(model.ApplicationRetentionTriggerManual); err == nil {
		t.Fatal("completion failure was ignored")
	}
}

func TestExecuteClassifiesRunFailures(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "timeout", err: context.DeadlineExceeded, expected: "timeout"},
		{name: "cancelled", err: context.Canceled, expected: "cancelled"},
		{name: "storage", err: errors.New("disk full"), expected: "storage"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := newStubStore()
			store.deleteErr = test.err
			service := New(t.Context(), store)
			if err := service.execute(model.ApplicationRetentionTriggerAutomatic); !errors.Is(err, test.err) {
				t.Fatalf("execute = %v", err)
			}
			if len(store.codes) != 1 || store.codes[0] != test.expected {
				t.Fatalf("codes = %v", store.codes)
			}
		})
	}
}

func TestPassAndDeleteAllHonourCancellation(t *testing.T) {
	broken := newStubStore()
	broken.getErr = errors.New("settings unavailable")
	if _, err := New(t.Context(), broken).pass(t.Context(), time.Now()); err == nil {
		t.Fatal("settings failure was ignored")
	}
	service := New(t.Context(), newStubStore())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.deleteAll(ctx, func(context.Context) (int64, error) {
		t.Fatal("canceled cleanup still issued a batch")
		return 0, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("deleteAll = %v", err)
	}
	failing := New(t.Context(), newStubStore())
	if _, err := failing.deleteAll(t.Context(), func(context.Context) (int64, error) {
		return 0, errors.New("batch rejected")
	}); err == nil {
		t.Fatal("batch failure was ignored")
	}
	stopping := New(t.Context(), newStubStore())
	stopCtx, stop := context.WithCancel(t.Context())
	total, err := stopping.deleteAll(stopCtx, func(context.Context) (int64, error) {
		stop()
		return int64(model.ApplicationRetentionBatchSize), nil
	})
	if !errors.Is(err, context.Canceled) || total != int64(model.ApplicationRetentionBatchSize) {
		t.Fatalf("deleteAll = %d %v", total, err)
	}
}

func TestRunDueExecutesScheduledCleanup(t *testing.T) {
	store := newStubStore()
	store.settings.AutoCleanupEnabled = true
	store.settings.CleanupHourUTC = 3
	service := New(t.Context(), store).WithClock(func() time.Time {
		return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	})
	if err := service.runDue(t.Context()); err != nil {
		t.Fatalf("runDue = %v", err)
	}
	if len(store.completed) != 1 || service.Running() {
		t.Fatalf("completed = %v running = %t", store.completed, service.Running())
	}
}

func TestErrorCodeClassifiesFixedCategories(t *testing.T) {
	if code := errorCode(context.DeadlineExceeded); code != "timeout" {
		t.Fatalf("code = %s", code)
	}
	if code := errorCode(context.Canceled); code != "cancelled" {
		t.Fatalf("code = %s", code)
	}
	if code := errorCode(errors.New("boom")); code != "storage" {
		t.Fatalf("code = %s", code)
	}
}
