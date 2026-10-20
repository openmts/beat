package agentupdate

import (
	"context"
	"errors"
	"io"
	"os"
)

func (u *Updater) Rollback(ctx context.Context) (Status, error) {
	if err := u.managementGate(); err != nil {
		return heldStatus(err), err
	}
	if !u.mu.TryLock() {
		return Status{}, ErrBusy
	}
	defer u.mu.Unlock()
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return Status{}, err
	}
	defer inst.close()
	value, err := readJournal(inst)
	if err != nil || value == nil {
		return statusOf(value), err
	}
	if err = u.validateJournal(value); err != nil {
		return statusOf(value), err
	}
	err = u.rollbackLocked(ctx, transaction{inst, value}, "rollback_requested")
	return statusOf(value), err
}

func (u *Updater) rollbackLocked(ctx context.Context, txn transaction, reason string) error {
	inst, value := txn.inst, txn.value
	state, err := u.currentState(ctx, inst, value)
	if err != nil {
		return err
	}
	if state == "previous" {
		return u.finishPrevious(ctx, txn, reason)
	}

	if err = u.stageRollback(ctx, inst, value); err != nil {
		return err
	}
	value.Phase, value.Reason, value.Restarted = "rollback_prepared", reason, false
	if err = saveJournal(inst, value); err != nil {
		return err
	}
	if err = u.checkpoint("rollback_prepared"); err != nil {
		return err
	}
	if err = u.replaceEntrypoint(ctx, inst, entryReplacement{source: rollbackFile, expected: imageFor(value), checkpoint: "rollback_rename"}); err != nil {
		return err
	}

	value.Phase = "rolled_back"
	if err = saveJournal(inst, value); err != nil {
		return err
	}
	u.observe("rolled_back", reason, 0)
	return u.restartJournal(ctx, inst, value)
}

func (u *Updater) stageRollback(ctx context.Context, inst *installation, value *journal) error {
	previous, err := inst.state.open(previousFile, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = previous.Close() }()
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, previous, value.Previous); err != nil {
		return err
	}
	if err = safeRemove(inst.state, rollbackFile); err != nil {
		return err
	}
	if _, err = previous.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err = privateCopy(io.LimitReader(&contextReader{ctx: ctx, reader: previous}, value.Previous.Size+1), inst.state, rollbackFile); err != nil {
		return err
	}
	ready, err := inst.state.open(rollbackFile, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = imageVerifier(u.config.Arch).makeExecutable(ctx, ready, value.Previous)
	return errors.Join(err, ready.Close())
}

func (u *Updater) finishPrevious(ctx context.Context, txn transaction, reason string) error {
	inst, value := txn.inst, txn.value
	if value.Phase == "rollback_prepared" {
		value.Phase = "rolled_back"
		value.Reason = reason
		if err := inst.root.sync(); err != nil {
			return err
		}
		if err := saveJournal(inst, value); err != nil {
			return err
		}
		return u.restartJournal(ctx, inst, value)
	}
	if value.Phase == "rolled_back" {
		return u.restartJournal(ctx, inst, value)
	}
	value.Phase, value.Reason = "canceled", reason
	return saveJournal(inst, value)
}
