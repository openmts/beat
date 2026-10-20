package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupervisorTerminalExitCancelsRootWithoutRestart(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint(panics), func(t *testing.T) {
			supervisor := newWorkerSupervisor()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			supervisor.bind(ctx, cancel)
			worker := supervisor.register("alerts", time.Minute)
			var wg sync.WaitGroup
			var calls atomic.Int64
			supervisor.start(&wg, worker, func(context.Context) {
				calls.Add(1)
				if panics {
					panic("sensitive panic payload")
				}
			})
			wg.Wait()
			if ctx.Err() == nil || calls.Load() != 1 || !supervisor.terminal.Load() {
				t.Fatal("unexpected exit did not terminate root")
			}
			value := worker.snapshot()
			if value.Running || value.Restarts != 0 || value.State != "failed" {
				t.Fatal(value)
			}
			if healthy, reason := worker.healthy(time.Now()); healthy || reason != "failed" {
				t.Fatal(healthy, reason)
			}
		})
	}
}

func TestSupervisorExpectedStopDoesNotFail(t *testing.T) {
	supervisor := newWorkerSupervisor()
	ctx, cancel := context.WithCancel(t.Context())
	supervisor.bind(ctx, cancel)
	worker := supervisor.register("maintenance", time.Minute)
	var wg sync.WaitGroup
	supervisor.start(&wg, worker, func(ctx context.Context) { <-ctx.Done() })
	cancel()
	wg.Wait()
	if supervisor.terminal.Load() || worker.snapshot().State != "stopped" {
		t.Fatal(worker.snapshot())
	}
}

func TestSupervisorObserverTracksSuccessAndFailures(t *testing.T) {
	supervisor := newWorkerSupervisor()
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := supervisor.register("observed", 10*time.Second)
	observe := worker.observe()
	supervisor.bind(ctx, cancel)
	supervisor.start(&wg, worker, func(context.Context) {
		<-ctx.Done()
	})

	failure := context.DeadlineExceeded
	for range 2 {
		observe(failure)
	}
	for _, snapshot := range supervisor.snapshot() {
		if snapshot.ConsecutiveFails != 2 {
			t.Fatalf("consecutive fails = %d, want 2", snapshot.ConsecutiveFails)
		}
		if !snapshot.LastSuccess.IsZero() {
			t.Fatal("no success should be recorded yet")
		}
	}

	observe(nil)
	for _, snapshot := range supervisor.snapshot() {
		if snapshot.ConsecutiveFails != 0 {
			t.Fatalf("success should reset fails, got %d", snapshot.ConsecutiveFails)
		}
		if snapshot.LastSuccess.IsZero() {
			t.Fatal("success should record last success timestamp")
		}
	}

	if unknown := supervisor.observer("missing"); unknown == nil {
		t.Fatal("unknown observer must not be nil")
	}
}

func TestWorkerHealthStates(t *testing.T) {
	interval := time.Minute
	now := time.Now().UTC()

	stopped := newSupervisedWorker("stopped-worker", interval)
	stopped.running.Store(false)

	failing := newSupervisedWorker("failing-worker", interval)
	failing.running.Store(true)
	failing.lastSuccess.Store(now.UnixNano())
	failing.consecutiveFails.Store(4)

	stale := newSupervisedWorker("stale-worker", interval)
	stale.running.Store(true)
	stale.lastSuccess.Store(now.Add(-10 * interval).UnixNano())

	healthy := newSupervisedWorker("healthy-worker", interval)
	healthy.running.Store(true)
	healthy.lastSuccess.Store(now.Add(-interval).UnixNano())

	testCases := []struct {
		name       string
		worker     *supervisedWorker
		wantOK     bool
		wantReason string
	}{
		{"stopped", stopped, false, "stopped"},
		{"failing", failing, false, "failing"},
		{"stale", stale, false, "stale"},
		{"healthy", healthy, true, ""},
	}
	for _, testCase := range testCases {
		ok, reason := testCase.worker.healthy(now)
		if ok != testCase.wantOK || reason != testCase.wantReason {
			t.Fatalf("%s: got ok=%v reason=%q, want %v/%q",
				testCase.name, ok, reason, testCase.wantOK, testCase.wantReason)
		}
	}
}

func TestSchedulerCheckReportsFailingAndStale(t *testing.T) {
	operations := newTestOperability()
	operations.runtime = newRuntimeStateWithWorkers("alerts", "traffic_reports", "maintenance")
	workers := operations.runtime.workers.workers
	// alerts 健康运行；traffic_reports 连续失败；maintenance 滞后
	workers[1].consecutiveFails.Store(4)
	workers[2].lastSuccess.Store(time.Now().Add(-2 * time.Hour).UnixNano())

	check := operations.schedulerCheck()
	if check.Status != "error" {
		t.Fatalf("scheduler check = %#v, want error", check)
	}
	for _, fragment := range []string{"traffic_reports failing", "maintenance stale"} {
		if !strings.Contains(check.Detail, fragment) {
			t.Fatalf("detail %q missing %q", check.Detail, fragment)
		}
	}
}

func TestSchedulerWorkersNilSupervisor(t *testing.T) {
	state := &runtimeState{}
	if workers := state.schedulerWorkers(); workers != nil {
		t.Fatalf("nil supervisor should yield no workers, got %#v", workers)
	}
	if health := state.schedulerHealth(time.Now()); health != nil {
		t.Fatalf("nil supervisor should yield no health, got %#v", health)
	}
}

func TestReadyzReportsNotReadyWhenWorkerExits(t *testing.T) {
	operations := newTestOperability()
	for i := range operations.runtime.workers.workers {
		operations.runtime.workers.workers[i].running.Store(false)
		operations.runtime.workers.workers[i].lastSuccess.Store(
			time.Now().Add(-time.Duration(i+1) * time.Minute).UnixNano())
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	operations.handleReady(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"worker_alerts"`) ||
		!strings.Contains(body, `"worker_traffic_reports"`) {
		t.Fatalf("readyz body %q should name stopped workers", body)
	}
}

func TestSchedulerWorkerGaugesInPrometheusOutput(t *testing.T) {
	operations := newTestOperability()
	worker := operations.runtime.workers.workers[0]
	worker.lastSuccess.Store(1700000000 * 1e9)
	worker.consecutiveFails.Store(1)
	worker.restarts.Store(2)

	metrics := operations.prometheus(context.Background())
	for _, expected := range []string{
		`beat_scheduler_worker_running{worker="alerts"} 1`,
		`beat_scheduler_worker_last_success_unix_seconds{worker="alerts"} 1700000000`,
		`beat_scheduler_worker_consecutive_failures{worker="alerts"} 1`,
		`beat_scheduler_worker_restarts_total{worker="alerts"} 2`,
		"beat_terminal_sessions_active 2",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestHealthzRemainsAvailable(t *testing.T) {
	operations := newTestOperability()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	mux := http.NewServeMux()
	operations.register(mux)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", response.Code)
	}
}
