package agentoperations

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func (runner *Runner) validateFileLease(lease protocol.Lease) error {
	if lease.Payload.File == nil || len(lease.Payload.Command) != 0 || lease.Payload.Limits.RunAsRoot {
		return ErrPolicy
	}
	request := protocol.FileRequest{Direction: lease.Payload.Limits.Kind, File: *lease.Payload.File}
	if err := protocol.AuthorizeFile(runner.settings.policy, runner.settings.policy, request); err != nil {
		return ErrPolicy
	}
	if _, ok := runner.gateway.(FileGateway); !ok {
		return ErrBoundary
	}
	return nil
}

func (runner *Runner) fileRoot(id string) (FileRoot, error) {
	for _, root := range runner.settings.config.FileRoots {
		if root.ID == id {
			return root, nil
		}
	}
	return FileRoot{}, ErrPolicy
}

func (runner *Runner) runTransfer(parent context.Context, record journal) error {
	if err := runner.validateLeasePayload(record.Lease); err != nil {
		return err
	}
	if err := runner.ensureTransferProgress(&record); err != nil {
		return err
	}
	finished, err := runner.prepareTransfer(parent, &record)
	if err != nil || finished {
		return err
	}
	job, err := runner.newTransferJob(&record)
	if err != nil {
		return err
	}
	return runner.executeTransfer(parent, &record, job)
}

func transferCommitted(record journal) bool {
	return record.Transfer.Phase == "published" || record.Transfer.Phase == "finalized"
}

func (runner *Runner) prepareTransfer(ctx context.Context, record *journal) (bool, error) {
	if transferCommitted(*record) {
		return true, runner.finishTransfer(ctx, *record, nil)
	}
	if time.Now().UnixMilli() < record.Transfer.StartedAt {
		return true, ErrStorage
	}
	if record.State != "leased" && time.Now().UnixMilli() >= record.Transfer.DeadlineAt {
		return true, runner.finishTransfer(ctx, *record, errTimeout)
	}
	if err := runner.gateway.Start(ctx, record.Lease); err != nil {
		if errors.Is(err, ErrLeaseRejected) {
			return true, runner.rejectTransfer(ctx, *record)
		}
		return true, err
	}
	record.State = "running"
	if err := runner.journals.save(*record); err != nil {
		return true, err
	}
	runner.emit(*record)
	if time.Now().UnixMilli() >= record.Transfer.DeadlineAt {
		return true, runner.finishTransfer(ctx, *record, errTimeout)
	}
	return false, nil
}

func (runner *Runner) rejectTransfer(ctx context.Context, record journal) error {
	if record.State == "leased" {
		return runner.journals.remove(record.Lease.AttemptID)
	}
	return runner.finishTransfer(ctx, record, errCanceled)
}

func (runner *Runner) executeTransfer(parent context.Context, record *journal, job transferJob) error {
	ctx, cancel := context.WithDeadlineCause(parent, time.UnixMilli(record.Transfer.DeadlineAt), errTimeout)
	defer cancel()
	heartbeatCtx, stopHeartbeat := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() { defer close(done); runner.heartbeats(heartbeatCtx, record.Lease, stopHeartbeat) }()
	transferErr := job.execute(heartbeatCtx)
	cause := context.Cause(heartbeatCtx)
	stopHeartbeat(nil)
	<-done
	if transferCommitted(*record) {
		return runner.finishTransfer(parent, *record, nil)
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	if cause != nil {
		transferErr = cause
	}
	if errors.Is(transferErr, ErrTransport) {
		return transferErr
	}
	return runner.finishTransfer(parent, *record, transferErr)
}

func (job transferJob) execute(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if job.record.Lease.Payload.Limits.Kind == "deploy" {
		return job.deploy(ctx)
	}
	if job.record.Transfer.Phase == "finalized" {
		return nil
	}
	if job.record.Transfer.Phase == "finalizing" {
		return job.finalizeCollect(ctx)
	}
	return job.collect(ctx)
}

func (runner *Runner) finishTransfer(ctx context.Context, record journal, transferErr error) error {
	started := time.Now().UTC()
	if record.Transfer != nil {
		started = time.UnixMilli(record.Transfer.StartedAt)
	}
	result := terminalResult("succeeded", started)
	result.ExitCode = 0
	if transferErr != nil {
		result.State, result.Reason = transferFailure(transferErr)
		result.ExitCode = -1
		if record.Transfer != nil && record.Transfer.Phase == "publishing" {
			result.State = "interrupted"
			result.Reason = "interrupted"
		}
		if err := runner.cleanupTransfer(record); err != nil {
			return err
		}
	}
	return runner.complete(ctx, record, result)
}

func transferFailure(err error) (string, string) {
	switch {
	case errors.Is(err, errCanceled):
		return "canceled", "canceled"
	case errors.Is(err, errTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timed_out", "timeout"
	case errors.Is(err, context.Canceled), errors.Is(err, errDisconnected):
		return "interrupted", "interrupted"
	case errors.Is(err, ErrFileIntegrity):
		return "failed", "file_integrity"
	case errors.Is(err, ErrSourceChanged):
		return "failed", "source_changed"
	case errors.Is(err, ErrFileConflict):
		return "failed", "transfer_conflict"
	case errors.Is(err, ErrPolicy):
		return "failed", "local_policy"
	default:
		return "failed", "storage_failure"
	}
}

func (runner *Runner) cleanupTransfer(record journal) (resultErr error) {
	progress := record.Transfer
	if progress == nil || progress.Temporary == "" || progress.Phase == "published" {
		return nil
	}
	configured, err := runner.fileRoot(record.Lease.Payload.File.RootID)
	if err != nil {
		return err
	}
	root, err := openTransferRoot(configured.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	destination, err := root.destination(record.Lease.Payload.File.RelativePath)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, destination.Close()) }()
	return destination.removeTemporary(progress.Temporary)
}

func (runner *Runner) ensureTransferProgress(record *journal) error {
	if record.Transfer != nil {
		return nil
	}
	record.Transfer = newTransferProgress(record.Lease)
	return runner.journals.save(*record)
}

func (runner *Runner) newTransferJob(record *journal) (transferJob, error) {
	root, err := runner.fileRoot(record.Lease.Payload.File.RootID)
	if err != nil {
		return transferJob{}, err
	}
	gateway, ok := runner.gateway.(FileGateway)
	if !ok {
		return transferJob{}, ErrBoundary
	}
	return transferJob{record: record, root: root, gateway: gateway, save: func() error { return runner.journals.save(*record) }}, nil
}
