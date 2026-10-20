package agentupdate

import (
	"context"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func (u *Updater) Observe(ctx context.Context, evidence HealthEvidence) (Status, error) {
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
	if !u.validObserverAnchor(value) {
		return statusOf(value), ErrHealth
	}
	err = u.observeLocked(ctx, transaction{inst, value}, evidence)
	return statusOf(value), err
}

func (u *Updater) observeLocked(ctx context.Context, txn transaction, evidence HealthEvidence) error {
	inst, value := txn.inst, txn.value
	var err error
	if err = u.verifyCurrent(ctx, inst, imageFor(value)); err != nil {
		return err
	}
	countersChanged, err := u.observeManagerRestarts(ctx, value)
	if err != nil {
		return err
	}
	evidence.managerCounted = countersChanged
	if value.Phase == "healthy" {
		return u.observeCompleted(txn, evidence, countersChanged)
	}
	err = u.observeHealth(ctx, transaction{inst, value}, evidence)
	return err
}

func (u *Updater) observeHealth(ctx context.Context, state transaction, evidence HealthEvidence) error {
	inst, value := state.inst, state.value
	now := u.config.Now()
	if !now.Before(value.Deadline) {
		return u.rollbackLocked(ctx, transaction{inst, value}, "watchdog_timeout")
	}
	if evidence.At.Before(value.PublishedAt) || evidence.At.After(now.Add(time.Second)) || evidence.At.Before(value.LastHealthyAt) {
		return ErrHealth
	}
	if !validHealthEvidence(evidence, value.TargetVersion) {
		return u.rollbackLocked(ctx, transaction{inst, value}, "watchdog_failed")
	}
	trackHealthProcess(value, evidence.ProcessID, evidence.managerCounted)
	if value.FirstHealthyAt.IsZero() || evidence.At.Sub(value.LastHealthyAt) > u.config.MaximumObservationGap {
		value.FirstHealthyAt = evidence.At
	}
	value.LastHealthyAt = evidence.At
	value.Phase = "observing"
	if evidence.At.Sub(value.FirstHealthyAt) >= u.config.ObservationWindow {
		value.Phase = "healthy"
	}
	if err := saveJournal(inst, value); err != nil {
		return err
	}
	u.observe(value.Phase, "", 0)
	return nil
}

func validHealthEvidence(evidence HealthEvidence, version string) bool {
	return protocol.ValidSigningKeyID(evidence.ProcessID) && protocol.SameReleaseVersion(evidence.Version, version) && evidence.ConfigParsed && evidence.IdentityLoaded && evidence.ServerAuthenticated && evidence.ValidSample
}

func awaitingHealth(phase string) bool { return phase == "published" || phase == "observing" }

func (u *Updater) observeCompleted(txn transaction, evidence HealthEvidence, countersChanged bool) error {
	inst, value := txn.inst, txn.value
	if !validHealthEvidence(evidence, value.TargetVersion) || evidence.At.Before(value.LastHealthyAt) || evidence.At.After(u.config.Now().Add(time.Second)) {
		return ErrHealth
	}
	if evidence.ProcessID == value.HealthProcess {
		if countersChanged {
			return saveJournal(inst, value)
		}
		return nil
	}
	value.HealthProcess = evidence.ProcessID
	if !countersChanged {
		value.Restarts++
	}
	return saveJournal(inst, value)
}

func trackHealthProcess(value *journal, processID string, automaticCounted bool) {
	if value.HealthProcess != processID {
		if value.HealthProcess != "" && !automaticCounted {
			value.Restarts++
		}
		value.HealthProcess = processID
		value.FirstHealthyAt = time.Time{}
	}
}

func (u *Updater) validObserverAnchor(value *journal) bool {
	return sameImage(u.config.Installed, imageFor(value)) && (awaitingHealth(value.Phase) || value.Phase == "healthy")
}
