package agentupdate

import (
	"context"
	"errors"
	"os"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func (supervisor *Supervisor) applyPending(ctx context.Context, updater *Updater, assignment protocol.Assignment) error {
	_, applyErr := updater.Apply(ctx, assignment)
	if applyErr == nil || !fatalTransfer(applyErr, updater, assignment) {
		return applyErr
	}
	status, cancelErr := updater.Cancel(ctx)
	if cancelErr != nil && !errors.Is(cancelErr, ErrBusy) {
		return errors.Join(applyErr, cancelErr)
	}
	if err := updater.discardPending(assignment); err != nil {
		return errors.Join(applyErr, err)
	}
	fetcher, ok := updater.config.Fetcher.(*HTTPFetcher)
	if !ok {
		return applyErr
	}
	version := updater.config.Installed.Version
	if status.Phase == "canceled" {
		version = status.PreviousVersion
	}
	observation := protocol.Observation{AssignmentID: assignment.ID, Nonce: assignment.Nonce, Version: version, ReportAt: updater.config.Now(), Outcome: "failed"}
	client := &ControlClient{fetcher: fetcher}
	return errors.Join(applyErr, client.Observe(ctx, observation))
}

func fatalTransfer(err error, updater *Updater, assignment protocol.Assignment) bool {
	return errors.Is(err, ErrIntegrity) || errors.Is(err, ErrFormat) || errors.Is(err, context.DeadlineExceeded) || !updater.config.Now().Before(assignment.ExpiresAt)
}

func (u *Updater) discardPending(assignment protocol.Assignment) error {
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return err
	}
	defer inst.close()
	if inst.state == nil {
		return nil
	}
	data, err := readPrivate(inst.state, pendingAssignmentFile, maximumJournalBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var current protocol.Assignment
	if err = decodePrivateJSON(data, &current); err != nil {
		return err
	}
	if !sameAssignment(current, assignment) {
		return ErrBusy
	}
	if err = safeRemove(inst.state, pendingAssignmentFile); err != nil {
		return err
	}
	return inst.state.sync()
}
