package main

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// 监督器重启策略：意外退出后按退避重启，超过次数后放弃并保持就绪失败。
const (
	defaultBackoffBase = time.Second
	defaultBackoffMax  = 30 * time.Second
	defaultMaxRestarts = 3
	staleMultiplier    = 4
)

// supervisedWorker 跟踪一个后台 worker 的真实运行状态与执行结果，使就绪检查
// 能反映 worker 意外退出、panic、连续失败与滞后，而不仅仅是“启动成功”。
type supervisedWorker struct {
	name             string
	expectedInterval time.Duration
	running          atomic.Bool
	lastSuccess      atomic.Int64 // Unix 纳秒；0 表示从未成功
	consecutiveFails atomic.Int64
	restarts         atomic.Int64
}

func newSupervisedWorker(name string, interval time.Duration) *supervisedWorker {
	return &supervisedWorker{name: name, expectedInterval: interval}
}

func (w *supervisedWorker) snapshot() workerSnapshot {
	nanos := w.lastSuccess.Load()
	var lastSuccess time.Time
	if nanos != 0 {
		lastSuccess = time.Unix(0, nanos)
	}
	return workerSnapshot{
		Name:             w.name,
		Running:          w.running.Load(),
		LastSuccess:      lastSuccess,
		ConsecutiveFails: w.consecutiveFails.Load(),
		Restarts:         w.restarts.Load(),
	}
}

// observe 返回供 worker 每次迭代结束后调用的结果钩子。
func (w *supervisedWorker) observe() func(error) {
	return func(err error) {
		if err != nil {
			w.consecutiveFails.Add(1)
			return
		}
		w.lastSuccess.Store(time.Now().UnixNano())
		w.consecutiveFails.Store(0)
	}
}

func (w *supervisedWorker) healthy(now time.Time) (bool, string) {
	if !w.running.Load() {
		return false, "stopped"
	}
	snapshot := w.snapshot()
	if snapshot.ConsecutiveFails >= staleMultiplier {
		return false, "failing"
	}
	if !snapshot.LastSuccess.IsZero() &&
		now.Sub(snapshot.LastSuccess) > staleMultiplier*w.expectedInterval {
		return false, "stale"
	}
	return true, ""
}

type workerSnapshot struct {
	Name             string
	Running          bool
	LastSuccess      time.Time
	ConsecutiveFails int64
	Restarts         int64
}

type workerSupervisor struct {
	mu          sync.Mutex
	workers     []*supervisedWorker
	backoffBase time.Duration
	backoffMax  time.Duration
	maxRestarts int
}

func newWorkerSupervisor() *workerSupervisor {
	return &workerSupervisor{
		backoffBase: defaultBackoffBase, backoffMax: defaultBackoffMax,
		maxRestarts: defaultMaxRestarts,
	}
}

// register 登记一个受监督 worker 并返回其句柄；调用方应在 start 之前把
// handle.observe() 挂到 worker 的结果回调上，避免启动后的数据竞争。
func (s *workerSupervisor) register(name string, expectedInterval time.Duration) *supervisedWorker {
	w := newSupervisedWorker(name, expectedInterval)
	s.mu.Lock()
	s.workers = append(s.workers, w)
	s.mu.Unlock()
	return w
}

// start 启动已注册的后台任务：panic 被捕获并记录；意外退出后按指数退避
// 有限次重启，耗尽后保持停止状态由就绪检查如实上报。
func (s *workerSupervisor) start(
	wg *sync.WaitGroup,
	ctx context.Context,
	w *supervisedWorker,
	fn func(context.Context),
) {
	backoffBase, backoffMax, maxRestarts := s.backoffBase, s.backoffMax, s.maxRestarts
	w.running.Store(true)
	wg.Go(func() {
		defer func() {
			w.running.Store(false)
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "background worker gave up after restarts",
					"worker", w.name, "restarts", w.restarts.Load())
			}
		}()
		backoff := backoffBase
		for attempt := 0; ; attempt++ {
			panicValue := runWorker(ctx, fn)
			if ctx.Err() != nil {
				return // 受控停机，不重启
			}
			if panicValue != nil {
				slog.ErrorContext(ctx, "background worker panicked",
					"worker", w.name, "panic", panicValue, "stack", string(debug.Stack()))
			} else {
				slog.ErrorContext(ctx, "background worker exited unexpectedly", "worker", w.name)
			}
			if attempt >= maxRestarts {
				return
			}
			slog.WarnContext(ctx, "restarting background worker",
				"worker", w.name, "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				w.running.Store(false)
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > backoffMax {
				backoff = backoffMax
			}
			w.restarts.Add(1)
			w.running.Store(true)
		}
	})
}

// runWorker 运行单轮 worker 并捕获 panic，供重启决策使用。
func runWorker(ctx context.Context, fn func(context.Context)) (panicValue any) {
	defer func() { panicValue = recover() }()
	fn(ctx)
	return nil
}

// observer 返回指定名称 worker 的结果钩子；未注册的名称返回空钩子。
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

type workerHealth struct {
	Name   string
	Reason string
}

// health 汇总每个 worker 的健康原因；空 Reason 表示健康。
func (s *workerSupervisor) health(now time.Time) []workerHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]workerHealth, 0, len(s.workers))
	for _, w := range s.workers {
		reason := ""
		if ok, why := w.healthy(now); !ok {
			reason = why
		}
		out = append(out, workerHealth{Name: w.name, Reason: reason})
	}
	return out
}
