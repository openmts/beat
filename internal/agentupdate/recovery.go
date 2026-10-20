package agentupdate

import (
	"context"
	"os"
)

func (u *Updater) Recover(ctx context.Context) (Status, error) {
	if err := u.managementGate(); err != nil {
		return heldStatus(err), err
	}
	if !u.mu.TryLock() {
		return Status{}, ErrBusy
	}
	defer u.mu.Unlock()
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return heldStatus(err), err
	}
	defer inst.close()
	if inst.state == nil {
		return Status{Phase: "idle"}, nil
	}
	if err = cleanupJournalTemporary(inst); err != nil {
		return Status{}, err
	}
	value, err := readJournal(inst)
	if err != nil || value == nil {
		return statusOf(value), err
	}
	if err = u.validateJournal(value); err != nil {
		return statusOf(value), err
	}
	err = u.recoverLocked(ctx, inst, value)
	return statusOf(value), err
}

func (u *Updater) recoverLocked(ctx context.Context, inst *installation, value *journal) error {
	current, err := u.currentState(ctx, inst, value)
	if err != nil {
		return err
	}
	if err = u.reconcilePhase(ctx, transaction{inst, value}, current); err != nil {
		return err
	}

	if (value.Phase == "published" || value.Phase == "observing") && !u.config.Now().Before(value.Deadline) {
		return u.rollbackLocked(ctx, transaction{inst, value}, "watchdog_timeout")
	}
	if value.Phase == "published" || value.Phase == "observing" || value.Phase == "rolled_back" {
		return u.restartJournal(ctx, inst, value)
	}
	return nil
}

func (u *Updater) currentState(ctx context.Context, inst *installation, value *journal) (string, error) {
	file, err := inst.root.open(inst.name, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, file, imageFor(value)); err == nil {
		return "target", nil
	}
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, file, value.Previous); err == nil {
		return "previous", nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", ErrConflict
}

func (u *Updater) Cancel(ctx context.Context) (Status, error) {
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
	if !prepublication(value.Phase) {
		return statusOf(value), ErrBusy
	}
	if err = u.verifyCurrent(ctx, inst, value.Previous); err != nil {
		return statusOf(value), err
	}
	if err = removeStaging(inst); err != nil {
		return statusOf(value), err
	}
	value.Phase, value.Reason = "canceled", "canceled"
	return statusOf(value), saveJournal(inst, value)
}

func (u *Updater) reconcilePhase(ctx context.Context, txn transaction, current string) error {
	inst, value := txn.inst, txn.value
	switch value.Phase {
	case "downloading", "canceled", "rolled_back":
		if current != "previous" {
			return ErrConflict
		}
	case "prepared":
		if current == "target" {
			value.Phase = "published"
			if err := inst.root.sync(); err != nil {
				return err
			}
			if err := saveJournal(inst, value); err != nil {
				return err
			}
		}
	case "rollback_prepared":
		return u.rollbackLocked(ctx, txn, value.Reason)
	case "published", "observing", "healthy":
		if current != "target" {
			return ErrConflict
		}
	}
	return nil
}

func prepublication(phase string) bool { return phase == "downloading" || phase == "prepared" }
