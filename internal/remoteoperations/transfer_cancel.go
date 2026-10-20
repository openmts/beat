package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) transferEvent(ctx context.Context, key string, mutate func(transaction) error) error {
	if service.queue == nil {
		return ErrUnavailable
	}
	_, err := service.queue.EnqueueOperation(ctx, notificationqueue.Request{SourceKey: "operation-transfer:" + key, SourceKind: "operation", SourceID: key,
		EventKind: "operation.failed", Subject: "Remote file transfer stopped", Text: "A remote file transfer requires attention in Beat.", UseSubscriptions: true},
		func(db *sql.Tx, access secretbox.ResourceAccess) error {
			return mutate(service.transaction(ctx, db, access))
		})
	return err
}

func (service *Service) CancelTransfer(ctx context.Context, actor Actor, id string) error {
	var scanID string
	err := service.transferEvent(ctx, "cancel:"+id, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		scanID, err = tx.stopTransfer(actor, id, "canceled")
		return err
	})
	if err == nil {
		service.stopScan(scanID)
	}
	return err
}

func (tx transaction) stopTransfer(actor Actor, id, state string) (string, error) {
	var artifactID, taskID sql.NullString
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT artifact_id,task_id FROM operation_transfers WHERE id=?", id).Scan(&artifactID, &taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrUnavailable
	}
	var scanID string
	if artifactID.Valid {
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT scan_id FROM operation_artifacts WHERE id=?", artifactID.String).Scan(&scanID); err != nil {
			return "", ErrUnavailable
		}
		if err := tx.one("UPDATE operation_artifacts SET state=?,scan_id='',scan_deadline=0,expires_at=?,updated_at=? WHERE id=?", state, tx.now.UnixMilli(), tx.now.UnixMilli(), artifactID.String); err != nil {
			return "", err
		}
	}
	if taskID.Valid {
		var active int
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_task_targets WHERE task_id=? AND state IN ('queued','leased','running','unknown','cancel_requested')", taskID.String).Scan(&active); err != nil {
			return "", ErrUnavailable
		}
		if active > 0 {
			if err := tx.cancelTask(actor, taskID.String); err != nil {
				return "", err
			}
		}
	}
	if err := tx.one("UPDATE operation_transfers SET state=?,updated_at=? WHERE id=?", state, tx.now.UnixMilli(), id); err != nil {
		return "", err
	}
	if err := tx.exec("UPDATE operation_transfer_targets SET state=? WHERE transfer_id=? AND state NOT IN ('succeeded','failed','canceled')", state, id); err != nil {
		return "", err
	}
	return scanID, tx.audit(actor.UserID, "operation.transfer.stopped", id)
}

func (service *Service) abandonScan(ctx context.Context, scan artifactScan, cause error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if errors.Is(cause, errArtifactCorrupt) || errors.Is(cause, errScanRejected) {
		return service.transferEvent(ctx, "scan:"+scan.id, func(tx transaction) error {
			if err := tx.ownsScan(scan); err != nil {
				return err
			}
			if _, err := tx.stopTransfer(scan.admission.actor, scan.admission.transferID, "corrupt"); err != nil {
				return err
			}
			return tx.one("UPDATE operation_artifacts SET scan_status='rejected' WHERE id=?", scan.item.id)
		})
	}
	return service.write(ctx, func(tx transaction) error {
		if err := tx.ownsScan(scan); err != nil {
			return err
		}
		return tx.one("UPDATE operation_artifacts SET state=?,scan_id='',scan_deadline=0,updated_at=? WHERE id=?", scan.previousState, tx.now.UnixMilli(), scan.item.id)
	})
}

func (tx transaction) ownsScan(scan artifactScan) error {
	var current string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT scan_id FROM operation_artifacts WHERE id=? AND state='scanning'", scan.item.id).Scan(&current); err != nil {
		return ErrConflict
	}
	if current != scan.id {
		return ErrConflict
	}
	return nil
}
