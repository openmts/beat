package agentdiagnostics

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

// errAlreadyRecorded 表示本地已有一条同 nonce 的记录；绝不重复执行同一次尝试。
var errAlreadyRecorded = errors.New("diagnostic attempt is already recorded locally")

// accept 领取一条租约；本地策略拒绝时也要明确上报失败，不能把作业留在队列里空等。
func (runner *Runner) accept(ctx context.Context, lease diagnosticprotocol.Lease) error {
	spec, err := runner.validateLease(lease)
	if err != nil {
		return runner.reject(ctx, lease)
	}
	if err := runner.record(lease); err != nil {
		if errors.Is(err, errAlreadyRecorded) {
			return nil
		}
		return err
	}
	runner.enter(lease.JobID)
	defer runner.leave(lease.JobID)
	return runner.execute(ctx, lease, spec)
}

// validateLease 把服务端参数收敛到本地白名单与本地上限；越界即拒绝。
func (runner *Runner) validateLease(lease diagnosticprotocol.Lease) (jobSpec, error) {
	if lease.JobID == "" || lease.Attempt < 1 || lease.Nonce == "" {
		return jobSpec{}, ErrPolicy
	}
	if _, ok := runner.settings.executors[lease.Payload.Kind]; !ok {
		return jobSpec{}, ErrPolicy
	}
	spec, err := specFromLease(runner.settings.config, &lease, runner.settings.work)
	if err != nil {
		return jobSpec{}, err
	}
	if err := runner.settings.admissibleTarget(spec.Destination); err != nil {
		return jobSpec{}, err
	}
	return spec, nil
}

// record 在执行前落私有 journal；崩溃后仍能证明这次尝试是否已经启动过。
func (runner *Runner) record(lease diagnosticprotocol.Lease) error {
	existing, err := runner.journals.load(lease.JobID, lease.Attempt)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.Nonce != lease.Nonce {
			return ErrPolicy
		}
		return errAlreadyRecorded
	}
	return runner.journals.save(journalEntry{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt,
		State: journalStateLeased})
}

// reject 把本地拒绝写成显式失败：先确认启动，再报告 local_policy 终态。
func (runner *Runner) reject(ctx context.Context, lease diagnosticprotocol.Lease) error {
	if err := runner.gateway.Start(ctx, attemptRequest(lease)); err != nil {
		return err
	}
	return runner.complete(ctx, lease, terminalOutcome(model.DiagnosticStateFailed, "local_policy"))
}

func (runner *Runner) execute(ctx context.Context, lease diagnosticprotocol.Lease, spec jobSpec) error {
	if err := runner.gateway.Start(ctx, attemptRequest(lease)); err != nil {
		return err
	}
	if err := runner.markStarted(lease); err != nil {
		return err
	}
	execution, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := runner.watch(execution, lease, cancel)
	result, execErr := runner.settings.executors[lease.Payload.Kind].Execute(execution, spec, runner.sink(execution, lease))
	stop()
	if execErr != nil {
		// 结果无法送达或本地无法确认时只报告 unknown，交由运维显式重试。
		result = terminalOutcome(model.DiagnosticStateUnknown, "report_failed")
	}
	return runner.complete(ctx, lease, result)
}

func (runner *Runner) markStarted(lease diagnosticprotocol.Lease) error {
	return runner.journals.save(journalEntry{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt,
		State: journalStateStarted})
}

// complete 报告终态并清理 journal；无法确认时不清理，让下次启动裁决成 unknown。
func (runner *Runner) complete(ctx context.Context, lease diagnosticprotocol.Lease, result outcome) error {
	completion := diagnosticprotocol.CompletionRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt,
		State: result.State, FailureCode: result.FailureCode, ActualBytes: result.ActualBytes}
	if err := runner.gateway.Complete(ctx, completion); err != nil {
		return err
	}
	runner.count(result.State)
	return runner.journals.remove(lease.JobID, lease.Attempt)
}

// watch 在后台发送心跳；服务端要求取消时立刻终止本地进程树。
func (runner *Runner) watch(ctx context.Context, lease diagnosticprotocol.Lease, cancel context.CancelFunc) func() {
	stopped := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(runner.settings.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-runner.lifetime.Done():
				cancel()
				return
			case <-ticker.C:
				if runner.beat(ctx, lease, cancel) {
					return
				}
			}
		}
	}()
	return func() {
		close(stopped)
		<-finished
	}
}

// beat 发送一次心跳；返回 true 表示本次执行应当结束。
func (runner *Runner) beat(ctx context.Context, lease diagnosticprotocol.Lease, cancel context.CancelFunc) bool {
	requested, err := runner.gateway.Heartbeat(ctx, attemptRequest(lease))
	if err != nil {
		runner.observe(err)
		return false
	}
	if !requested {
		return false
	}
	runner.observe(ErrCancelled)
	cancel()
	return true
}

// sink 把执行结果拆成受限批次：序号单调递增，只承认服务端已确认的序号。
func (runner *Runner) sink(ctx context.Context, lease diagnosticprotocol.Lease) sink {
	var sequence int64
	return func(frame batch) error {
		for index, chunk := range progressBatches(frame) {
			sequence++
			request := diagnosticprotocol.ProgressRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt,
				Samples: chunk, Sequence: sequence}
			if index == 0 {
				request.Hops = frame.Hops
			}
			acknowledged, err := runner.gateway.Progress(ctx, request)
			if err != nil {
				return err
			}
			if acknowledged < sequence {
				return ErrTransport
			}
		}
		return nil
	}
}

// progressBatches 把一次执行结果切成服务端允许的样本批次；跳身份跟随第一批。
func progressBatches(frame batch) [][]diagnosticprotocol.Sample {
	if len(frame.Samples) == 0 {
		if len(frame.Hops) == 0 {
			return nil
		}
		return [][]diagnosticprotocol.Sample{nil}
	}
	count := (len(frame.Samples) + maximumSamplesPerBatch - 1) / maximumSamplesPerBatch
	batches := make([][]diagnosticprotocol.Sample, 0, count)
	for start := 0; start < len(frame.Samples); start += maximumSamplesPerBatch {
		batches = append(batches, frame.Samples[start:min(start+maximumSamplesPerBatch, len(frame.Samples))])
	}
	return batches
}

// recover 裁决上次进程遗留的尝试：绝不重复执行，只把它标记为 unknown。
func (runner *Runner) recover(ctx context.Context) error {
	entries, err := runner.journals.pending()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := runner.abandon(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func (runner *Runner) abandon(ctx context.Context, entry journalEntry) error {
	completion := diagnosticprotocol.CompletionRequest{JobID: entry.JobID, Nonce: entry.Nonce, Attempt: entry.Attempt,
		State: model.DiagnosticStateUnknown, FailureCode: "agent_restarted"}
	err := runner.gateway.Complete(ctx, completion)
	if err != nil && !errors.Is(err, ErrPolicy) {
		return err
	}
	runner.count(model.DiagnosticStateUnknown)
	return runner.journals.remove(entry.JobID, entry.Attempt)
}

func attemptRequest(lease diagnosticprotocol.Lease) diagnosticprotocol.AttemptRequest {
	return diagnosticprotocol.AttemptRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt}
}
