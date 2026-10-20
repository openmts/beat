package agentupdate

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

var ErrManager = errors.New("agent update service manager failed")

const restartTimeout = 10 * time.Second

func (u *Updater) Apply(ctx context.Context, assignment protocol.Assignment) (Status, error) {
	if !u.config.Enabled {
		return Status{Phase: "disabled"}, ErrDisabled
	}
	if err := u.managementGate(); err != nil {
		return heldStatus(err), err
	}
	verified, err := u.verify(assignment, u.config.Now())
	if err != nil {
		return Status{Phase: "rejected", Reason: "assignment_rejected"}, err
	}
	if !u.mu.TryLock() {
		return Status{}, ErrBusy
	}
	defer u.mu.Unlock()
	inst, err := openInstallation(u.config.Executable, true)
	if err != nil {
		return heldStatus(err), err
	}
	defer inst.close()
	value, err := u.begin(inst, verified)
	if err != nil {
		return Status{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, value.VerifiedAt.Add(u.config.DownloadTimeout).Sub(u.config.Now()))
	defer cancel()
	err = u.applyLocked(bounded, inst, value)
	if err != nil {
		u.observe(value.Phase, "update_failed", value.Downloaded)
	}
	return statusOf(value), err
}

func (u *Updater) applyLocked(ctx context.Context, inst *installation, value *journal) error {
	if value.Phase == "published" || value.Phase == "observing" {
		return u.verifyCurrent(ctx, inst, imageFor(value))
	}
	if value.Phase == "downloading" {
		if err := u.downloadAndPrepare(ctx, inst, value); err != nil {
			return err
		}
	}

	if value.Phase != "prepared" {
		return ErrBusy
	}
	if _, err := u.verify(value.Assignment, u.config.Now()); err != nil {
		return err
	}
	if err := u.publish(ctx, inst, value); err != nil {
		return err
	}
	if err := u.restartJournal(ctx, inst, value); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		rollbackErr := u.rollbackLocked(rollbackCtx, transaction{inst, value}, "restart_failed")
		return errors.Join(err, rollbackErr)
	}
	return u.checkpoint("restart_requested")
}

func (u *Updater) verifyCurrent(ctx context.Context, inst *installation, image Image) error {
	file, err := inst.root.open(inst.name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o500 != 0o500 {
		return ErrManualOnly
	}
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, file, image); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrConflict
	}
	return nil
}

func checkSpace(inst *installation, value *journal) error {
	available, err := availableSpace(inst.root)
	if err != nil {
		return err
	}
	required := uint64(value.Assignment.Artifact.Size - value.Downloaded + value.Previous.Size + maximumJournalBytes)
	if available < required {
		return ErrStorage
	}
	return nil
}

func (u *Updater) publish(ctx context.Context, inst *installation, value *journal) error {
	if err := u.verifyCurrent(ctx, inst, value.Previous); err != nil {
		return err
	}
	if err := u.verifyCandidate(ctx, inst, value); err != nil {
		return err
	}
	var err error
	if value.PublishedAt.IsZero() {
		value.PublishedAt = u.config.Now()
		value.Deadline = value.PublishedAt.Add(u.config.StartupTimeout + u.config.ObservationWindow)
	}
	if err = saveJournal(inst, value); err != nil {
		return err
	}
	if err = u.checkpoint("publish_intent"); err != nil {
		return err
	}
	if err = u.replaceEntrypoint(ctx, inst, entryReplacement{source: downloadFile, expected: value.Previous, checkpoint: "published_rename"}); err != nil {
		return err
	}

	value.Phase = "published"
	if err = saveJournal(inst, value); err != nil {
		return err
	}
	u.observe("published", "", value.Downloaded)
	return u.checkpoint("published")
}

func (u *Updater) restart(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), restartTimeout)
	defer cancel()
	if err := u.config.Manager.Restart(bounded); err != nil {
		return ErrManager
	}
	return nil
}

func heldStatus(err error) Status {
	if errors.Is(err, ErrExternalManager) {
		return Status{Phase: "external_manager", Reason: "external_manager"}
	}
	return Status{Phase: "manual_only", Reason: "local_admission_failed"}
}

func (u *Updater) restartJournal(ctx context.Context, inst *installation, value *journal) error {
	if value.Restarted {
		return nil
	}
	value.Restarts++
	if err := saveJournal(inst, value); err != nil {
		return err
	}
	if err := u.restart(ctx); err != nil {
		return err
	}
	value.Restarted = true
	return saveJournal(inst, value)
}

func (u *Updater) downloadAndPrepare(ctx context.Context, inst *installation, value *journal) error {
	if err := u.verifyCurrent(ctx, inst, value.Previous); err != nil {
		return err
	}
	if err := checkSpace(inst, value); err != nil {
		return err
	}
	if err := u.download(ctx, inst, value); err != nil {
		return err
	}
	return u.prepare(ctx, inst, value)
}

func (u *Updater) verifyCandidate(ctx context.Context, inst *installation, value *journal) error {
	candidate, err := inst.state.open(downloadFile, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = imageVerifier(u.config.Arch).verifyImage(ctx, candidate, imageFor(value))
	if err = errors.Join(err, candidate.Close()); err != nil {
		return err
	}

	return nil
}

type entryReplacement struct {
	source     string
	expected   Image
	checkpoint string
}

func (u *Updater) replaceEntrypoint(ctx context.Context, inst *installation, replacement entryReplacement) error {
	if err := u.verifyCurrent(ctx, inst, replacement.expected); err != nil {
		return err
	}
	if err := renameRelative(fileRef{inst.state, replacement.source}, fileRef{inst.root, inst.name}); err != nil {
		return err
	}
	if err := u.checkpoint(replacement.checkpoint); err != nil {
		return err
	}
	return inst.root.sync()
}
