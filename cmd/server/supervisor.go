package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const staleMultiplier = 4

const startupGrace = 30 * time.Second

type supervisedWorker struct {
	name             string
	expectedInterval time.Duration
	running          atomic.Bool
	started          atomic.Bool
	failed           atomic.Bool
	panicked         atomic.Bool
	lastSuccess      atomic.Int64
	lastAttempt      atomic.Int64
	lastFailure      atomic.Int64
	startedAt        atomic.Int64
	stoppedAt        atomic.Int64
	consecutiveFails atomic.Int64
	failures         atomic.Int64
	restarts         atomic.Int64
	ctx              context.Context
}

func newSupervisedWorker(name string, interval time.Duration) *supervisedWorker {
	return &supervisedWorker{name: name, expectedInterval: interval}
}

func atomicTime(value *atomic.Int64) time.Time {
	if n := value.Load(); n != 0 {
		return time.Unix(0, n).UTC()
	}
	return time.Time{}
}

func (w *supervisedWorker) snapshot() workerSnapshot {
	value := workerSnapshot{Name: w.name, Configured: true, Started: w.started.Load(), Running: w.running.Load(), LastSuccess: atomicTime(&w.lastSuccess), LastAttempt: atomicTime(&w.lastAttempt), LastFailure: atomicTime(&w.lastFailure), StartedAt: atomicTime(&w.startedAt), StoppedAt: atomicTime(&w.stoppedAt), ConsecutiveFails: w.consecutiveFails.Load(), Restarts: w.restarts.Load(), Failures: w.failures.Load(), IntervalSeconds: w.expectedInterval.Seconds(), State: "starting"}
	if w.failed.Load() {
		value.State = "failed"
		value.Failure = "unexpected_exit"
		if w.panicked.Load() {
			value.Failure = "panic"
		}
		return value
	}
	if !value.Running && value.Started {
		value.State = "stopped"
		return value
	}
	if value.ConsecutiveFails > 0 {
		value.Failure = "iteration_failed"
	}
	if ok, _ := w.healthy(time.Now()); !ok && value.Running {
		value.State = "degraded"
	} else if !value.LastSuccess.IsZero() {
		value.State = "healthy"
	}
	value.SchedulerLagSeconds = workerSchedulerLag(value)
	return value
}

func (w *supervisedWorker) observe() func(error) {
	return func(err error) {
		if errors.Is(err, context.Canceled) || w.ctx != nil && w.ctx.Err() != nil {
			return
		}
		now := time.Now().UnixNano()
		w.lastAttempt.Store(now)
		if err != nil {
			w.consecutiveFails.Add(1)
			w.failures.Add(1)
			w.lastFailure.Store(now)
			return
		}
		w.lastSuccess.Store(now)
		w.consecutiveFails.Store(0)
	}
}

func (w *supervisedWorker) healthy(now time.Time) (bool, string) {
	if w.failed.Load() {
		return false, "failed"
	}
	if !w.running.Load() {
		return false, "stopped"
	}
	if w.consecutiveFails.Load() >= staleMultiplier {
		return false, "failing"
	}
	success := atomicTime(&w.lastSuccess)
	if !success.IsZero() && now.Sub(success) > staleMultiplier*w.expectedInterval {
		return false, "stale"
	}
	if success.IsZero() && now.Sub(atomicTime(&w.startedAt)) > max(startupGrace, 2*w.expectedInterval) {
		return false, "starting"
	}
	return true, ""
}

type workerSnapshot struct {
	Failures            int64     `json:"failures"`
	Name                string    `json:"name"`
	Configured          bool      `json:"configured"`
	Started             bool      `json:"started"`
	Running             bool      `json:"running"`
	StartedAt           time.Time `json:"started_at"`
	StoppedAt           time.Time `json:"stopped_at"`
	LastAttempt         time.Time `json:"last_attempt"`
	LastSuccess         time.Time `json:"last_success"`
	LastFailure         time.Time `json:"last_failure"`
	ConsecutiveFails    int64     `json:"consecutive_failures"`
	Restarts            int64     `json:"restarts"`
	IntervalSeconds     float64   `json:"interval_seconds"`
	SchedulerLagSeconds float64   `json:"scheduler_lag_seconds"`
	State               string    `json:"state"`
	Failure             string    `json:"failure"`
}

type workerSupervisor struct {
	mu       sync.Mutex
	workers  []*supervisedWorker
	ctx      context.Context
	cancel   context.CancelFunc
	terminal atomic.Bool
	stopping atomic.Bool
}

func newWorkerSupervisor() *workerSupervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &workerSupervisor{ctx: ctx, cancel: cancel}
}

func (s *workerSupervisor) bind(ctx context.Context, cancel context.CancelFunc) {
	s.ctx = ctx
	s.cancel = cancel
}

func (s *workerSupervisor) register(name string, interval time.Duration) *supervisedWorker {
	w := newSupervisedWorker(name, interval)
	s.mu.Lock()
	s.workers = append(s.workers, w)
	s.mu.Unlock()
	return w
}

func (s *workerSupervisor) start(wg *sync.WaitGroup, w *supervisedWorker, fn func(context.Context)) {
	if w.ctx == nil {
		w.ctx = s.ctx
	}
	ctx := w.ctx
	w.started.Store(true)
	w.startedAt.Store(time.Now().UnixNano())
	w.running.Store(true)
	wg.Go(func() {
		panicValue := runWorker(ctx, fn)
		w.stoppedAt.Store(time.Now().UnixNano())
		w.running.Store(false)
		if ctx.Err() != nil || s.stopping.Load() {
			return
		}
		w.failed.Store(true)
		if panicValue != nil {
			w.panicked.Store(true)
		}
		w.lastFailure.Store(time.Now().UnixNano())
		reason := "unexpected_exit"
		if w.panicked.Load() {
			reason = "panic"
		}
		// panic载荷可能包含secret；只输出固定故障枚举，不输出载荷或堆栈。
		slog.ErrorContext(ctx, "background worker terminated", "worker", w.name, "reason", reason)
		s.terminal.Store(true)
		s.cancel()
	})
}

func runWorker(ctx context.Context, fn func(context.Context)) (panicValue any) {
	defer func() { panicValue = recover() }()
	fn(ctx)
	return nil
}

func (s *workerSupervisor) observer(name string) func(error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workers {
		if w.name == name {
			return w.observe()
		}
	}
	return func(error) {}
}

func (s *workerSupervisor) snapshot() []workerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]workerSnapshot, 0, len(s.workers))
	for _, w := range s.workers {
		out = append(out, w.snapshot())
	}
	return out
}

type workerHealth struct{ Name, Reason string }

func (s *workerSupervisor) health(now time.Time) []workerHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]workerHealth, 0, len(s.workers))
	for _, w := range s.workers {
		_, reason := w.healthy(now)
		out = append(out, workerHealth{Name: w.name, Reason: reason})
	}
	return out
}

func (w *supervisedWorker) in(ctx context.Context) *supervisedWorker { w.ctx = ctx; return w }

func workerSchedulerLag(value workerSnapshot) float64 {
	attempt := value.LastAttempt
	if attempt.IsZero() {
		attempt = value.StartedAt
	}
	if !value.Running || attempt.IsZero() {
		return 0
	}
	return max(0, time.Since(attempt).Seconds()-value.IntervalSeconds)
}
