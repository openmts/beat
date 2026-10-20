package agentdiagnostics

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RunnerConfig 是 Agent 诊断运行时的外部依赖。
type RunnerConfig struct {
	Local   Config
	Gateway Gateway
	OnError func(error)
}

// Runner 轮询服务端派发的诊断作业，并只在本地白名单与本地上限内执行。
type Runner struct {
	settings  settings
	gateway   Gateway
	journals  *journal
	onError   func(error)
	lifetime  context.Context
	stop      context.CancelFunc
	workers   sync.WaitGroup
	closeOnce sync.Once
	closeErr  error

	mu        sync.Mutex
	closed    bool
	active    map[string]struct{}
	lastError string
	completed int64
	failed    int64
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	prepared, err := prepareSettings(config.Local)
	if err != nil {
		return nil, err
	}
	runner := &Runner{settings: prepared, gateway: config.Gateway, onError: config.OnError, active: map[string]struct{}{}}
	runner.lifetime, runner.stop = context.WithCancel(context.Background())
	if !prepared.config.Enabled {
		return runner, nil
	}
	if config.Gateway == nil {
		return nil, ErrTransport
	}
	journals, err := openJournal(prepared.journal)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(prepared.work); err != nil {
		return nil, err
	}
	runner.journals = journals
	return runner, nil
}

func (runner *Runner) Close() error {
	runner.closeOnce.Do(func() {
		runner.mu.Lock()
		runner.closed = true
		runner.stop()
		runner.mu.Unlock()
		runner.workers.Wait()
	})
	return runner.closeErr
}

// Run 先裁决上次进程遗留的尝试，再按并发上限持续轮询。
func (runner *Runner) Run(ctx context.Context) error {
	if !runner.settings.config.Enabled {
		return nil
	}
	runner.observe(runner.recover(ctx))
	for range runner.settings.config.MaximumConcurrent {
		runner.workers.Add(1)
		go func() { runner.loop(ctx); runner.workers.Done() }()
	}
	runner.workers.Wait()
	return nil
}

func (runner *Runner) loop(ctx context.Context) {
	for ctx.Err() == nil && runner.lifetime.Err() == nil {
		runner.observe(runner.RunOnce(ctx))
		if !runner.pause(ctx) {
			return
		}
	}
}

func (runner *Runner) pause(ctx context.Context) bool {
	timer := time.NewTimer(runner.settings.poll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-runner.lifetime.Done():
		return false
	case <-timer.C:
		return true
	}
}

// RunOnce 上报能力、尝试领取一条租约，并在本地执行它。
func (runner *Runner) RunOnce(ctx context.Context) error {
	if !runner.settings.config.Enabled || runner.lifetime.Err() != nil {
		return nil
	}
	digest, err := runner.gateway.Advertise(ctx, runner.settings.capability())
	if err != nil {
		return err
	}
	lease, err := runner.gateway.Lease(ctx, digest)
	if err != nil || lease == nil {
		return err
	}
	return runner.accept(ctx, *lease)
}

func (runner *Runner) observe(err error) {
	code := ErrorCode(err)
	runner.mu.Lock()
	changed := runner.lastError != code
	runner.lastError = code
	runner.mu.Unlock()
	if err != nil && changed && runner.onError != nil {
		runner.onError(callbackError(code))
	}
}

type callbackError string

func (err callbackError) Error() string { return string(err) }

// ErrorCode 把内部错误收敛成固定的可观测代码。
func ErrorCode(err error) string {
	if encoded, ok := err.(callbackError); ok {
		return string(encoded)
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrPolicy):
		return "local_policy"
	case errors.Is(err, ErrStorage):
		return "storage_failure"
	case errors.Is(err, ErrExecution):
		return "execution_failed"
	case errors.Is(err, ErrCancelled), errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrTransport):
		return "transport_unavailable"
	default:
		return "diagnostic_failed"
	}
}
