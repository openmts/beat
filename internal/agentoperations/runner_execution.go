package agentoperations

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func (runner *Runner) resume(ctx context.Context, record journal) error {
	if isFileLease(record.Lease) && record.State != "completed" {
		return runner.runTransfer(ctx, record)
	}
	switch record.State {
	case "leased":
		return runner.start(ctx, record)
	case "authorized", "running":
		if err := runner.engine.Recover(record.Lease.AttemptID); err != nil {
			return err
		}
		result := terminalResult("interrupted", time.Now().UTC())
		return runner.complete(ctx, record, result)
	case "completed":
		return runner.deliver(ctx, record)
	default:
		return ErrStorage
	}
}

func (runner *Runner) start(ctx context.Context, record journal) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := runner.validateLeasePayload(record.Lease); err != nil {
		return err
	}
	if err := runner.gateway.Start(ctx, record.Lease); err != nil {
		if errors.Is(err, ErrLeaseRejected) {
			return runner.journals.remove(record.Lease.AttemptID)
		}
		return err
	}
	script := record.Lease.Payload.Command
	record.State = "authorized"
	record.Lease.Payload.Command = nil
	if err := runner.journals.save(record); err != nil {
		return err
	}
	runner.emit(record)
	executionLease := record.Lease
	executionLease.Payload.Command = script
	child, cancel := context.WithTimeoutCause(ctx, time.Duration(executionLease.Payload.Limits.TimeoutSeconds)*time.Second, errTimeout)
	defer cancel()
	heartbeatCtx, stopHeartbeat := context.WithCancelCause(child)
	heartbeatDone := make(chan struct{})
	go func() { defer close(heartbeatDone); runner.heartbeats(heartbeatCtx, executionLease, stopHeartbeat) }()
	result, executionErr := runner.engine.Run(heartbeatCtx, execution{lease: executionLease, directory: runner.journals.directory(record.Lease.AttemptID), running: func() error {
		record.State = "running"
		if err := runner.journals.save(record); err != nil {
			return err
		}
		runner.emit(record)
		return nil
	}})
	stopHeartbeat(nil)
	<-heartbeatDone
	clear(script)
	if executionErr != nil {
		result.State = "interrupted"
		if errors.Is(executionErr, ErrStorage) {
			result.Reason = "storage_failure"
		}
		if errors.Is(executionErr, ErrBoundary) {
			result.Reason = "sandbox_unavailable"
		}
	}
	return runner.complete(ctx, record, result)
}

func (runner *Runner) heartbeats(ctx context.Context, lease protocol.Lease, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(runner.heartbeat)
	defer ticker.Stop()
	acknowledged := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			canceled, err := runner.gateway.Heartbeat(ctx, lease)
			if err == nil {
				acknowledged = time.Now()
			}
			if canceled {
				cancel(errCanceled)
				return
			}
			if time.Since(acknowledged) > protocol.HeartbeatGrace {
				cancel(errDisconnected)
				return
			}
		}
	}
}

func (runner *Runner) complete(ctx context.Context, record journal, result protocol.Result) error {
	record.State = "completed"
	record.Result = &result
	if err := runner.journals.save(record); err != nil {
		return err
	}
	runner.emit(record)
	return runner.deliver(ctx, record)
}

func (runner *Runner) deliver(ctx context.Context, record journal) error {
	completion := protocol.Completion{Attempt: protocol.AttemptRequest{ID: record.Lease.AttemptID, Nonce: record.Lease.Nonce}, Result: *record.Result}
	if err := runner.gateway.Complete(ctx, completion); err != nil {
		return err
	}
	if err := runner.journals.markAcknowledged(record.Lease.AttemptID); err != nil {
		return err
	}
	return runner.journals.remove(record.Lease.AttemptID)
}
