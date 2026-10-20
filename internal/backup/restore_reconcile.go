package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/remoteoperations"
	"github.com/beat/backend/internal/store"
)

type restoreApplication struct {
	dataDir, dbPath, mtsPath, journalPath string
	journal                               model.RestoreJournal
}

func (applier *Applier) applyValidated(ctx context.Context, archive *validatedArchive, input restoreApplication) error {
	at := applier.now()
	token := strings.ReplaceAll(journalTimestamp(at), ":", "")
	prepared, err := prepareRestore(archive, input.dbPath, input.mtsPath, input.dataDir, token)
	if err != nil {
		return err
	}
	defer prepared.cleanup()
	if err := reconcileRestoredState(ctx, prepared.targets[0].new, store.RestoredAuthentication{BackupID: input.journal.BackupID, At: at}); err != nil {
		return err
	}
	rollbacks, err := applier.activate(ctx, prepared, token)
	if err != nil {
		return err
	}
	if err := validateApplied(ctx, input.dbPath, input.mtsPath); err != nil {
		return errors.Join(err, applier.rollbackApplied(prepared, rollbacks))
	}
	if err := applier.completeJournal(input.journalPath, &input.journal, rollbacks, input.dbPath, input.mtsPath); err != nil {
		return errors.Join(err, applier.rollbackApplied(prepared, rollbacks))
	}
	prepared.committed = true
	return nil
}

func reconcileRestoredState(ctx context.Context, path string, restore store.RestoredAuthentication) error {
	if err := invalidateEnrollmentCredentials(ctx, path); err != nil {
		return err
	}
	if err := invalidateRestoredAuthentication(ctx, path, restore); err != nil {
		return err
	}
	reconcilers := []struct {
		name string
		run  func(context.Context, string, time.Time) error
	}{
		{"notification queue", notificationqueue.ReconcileRestore},
		{"remote operations", remoteoperations.ReconcileRestore},
		{"Agent rollouts", agentrollout.ReconcileRestore},
		{"GeoIP", reconcileGeoIPRestore},
		{"metric erasure", store.ReconcileMetricErasureRestore},
		{"fleet summaries", store.ReconcileFleetSummaryRestore},
	}
	for _, reconciler := range reconcilers {
		if err := reconciler.run(ctx, path, restore.At); err != nil {
			return fmt.Errorf("reconcile restored %s: %w", reconciler.name, err)
		}
	}
	return nil
}
