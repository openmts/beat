package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/beat/backend/internal/notificationqueue"
)

type recoveryArtifact struct {
	id, transferID, state string
	expires               int64
}

// Recover 在监听之前及周期维护时调用；不会重新执行已 durable-start 的任务。
func (service *Service) Recover(ctx context.Context) error {
	if err := service.atomicWrite(ctx, func(tx transaction) error { return tx.reconcile() }); err != nil {
		return err
	}
	if err := service.expireQueuedTasks(ctx); err != nil {
		return err
	}
	if err := service.atomicWrite(ctx, func(tx transaction) error { return tx.retainOperations() }); err != nil {
		return err
	}
	if service.spool == nil {
		return nil
	}
	items, err := service.recoveryArtifacts(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := service.recoverArtifact(ctx, item); err != nil {
			return err
		}
	}
	return service.atomicWrite(ctx, func(tx transaction) error { return tx.reconcileSpoolFiles(service.spool) })
}

func (service *Service) recoveryArtifacts(ctx context.Context) (items []recoveryArtifact, resultErr error) {
	rows, err := service.db.QueryContext(ctx, `SELECT a.id,COALESCE(t.id,''),a.state,a.expires_at FROM operation_artifacts a
 LEFT JOIN operation_transfers t ON t.artifact_id=a.id ORDER BY a.id LIMIT 257`)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var item recoveryArtifact
		if err := rows.Scan(&item.id, &item.transferID, &item.state, &item.expires); err != nil {
			return nil, ErrUnavailable
		}
		items = append(items, item)
	}
	if len(items) > 256 {
		return nil, ErrUnavailable
	}
	return items, rows.Err()
}

func (service *Service) recoverArtifact(ctx context.Context, item recoveryArtifact) error {
	return service.atomicMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		err := tx.tx.QueryRowContext(tx.ctx, `SELECT a.state,a.expires_at,COALESCE(t.id,'') FROM operation_artifacts a LEFT JOIN operation_transfers t ON t.artifact_id=a.id WHERE a.id=?`, item.id).Scan(&item.state, &item.expires, &item.transferID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, ErrUnavailable
		}
		missing, err := tx.spoolMissing(service.spool, item.id)
		if err != nil {
			return nil, err
		}
		terminal := item.state == "canceled" || item.state == "corrupt" || item.state == "expired"
		if !missing && !terminal && item.expires > tx.now.UnixMilli() {
			return nil, tx.recoverScan(service, item.id)
		}
		failure := missing || item.state == "uploading" || item.state == "awaiting_source" || item.state == "scanning"
		var active int
		if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM operation_task_targets n JOIN operation_transfers t ON t.task_id=n.task_id WHERE t.artifact_id=? AND n.state IN ('queued','leased','running','unknown','cancel_requested')`, item.id).Scan(&active); err != nil {
			return nil, ErrUnavailable
		}
		failure = failure || active > 0
		if item.transferID != "" && failure {
			state := "expired"
			if missing {
				state = "corrupt"
			}
			if _, err := tx.stopTransfer(Actor{}, item.transferID, state); err != nil {
				return nil, err
			}
		}
		if err := tx.expireArtifact(service.spool, item.id); err != nil {
			return nil, err
		}
		if err := tx.audit("", "operation.artifact.expired", item.id); err != nil {
			return nil, err
		}
		if failure {
			return operationFailure("recovery:"+item.id, item.id), nil
		}
		return nil, nil
	})
}

func (tx transaction) recoverScan(service *Service, id string) error {
	var scanID, state string
	var deadline int64
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT state,scan_id,scan_deadline FROM operation_artifacts WHERE id=?", id).Scan(&state, &scanID, &deadline); err != nil {
		return ErrUnavailable
	}
	if state != "scanning" {
		return nil
	}
	service.scanMu.Lock()
	_, active := service.scans[scanID]
	service.scanMu.Unlock()
	if active && deadline > tx.now.UnixMilli() {
		return nil
	}
	service.stopScan(scanID)
	return tx.one("UPDATE operation_artifacts SET state='uploading',scan_id='',scan_deadline=0,updated_at=? WHERE id=?", tx.now.UnixMilli(), id)
}

func (tx transaction) spoolMissing(spool *Spool, id string) (missing bool, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT storage_name FROM operation_artifact_chunks WHERE artifact_id=?", id)
	if err != nil {
		return false, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	count := 0
	for rows.Next() {
		count++
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, ErrUnavailable
		}
		info, err := spool.root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			missing = true
			continue
		}
		if err != nil {
			return false, ErrUnavailable
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > SpoolChunkBytes+128 {
			missing = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, ErrUnavailable
	}
	if err := rows.Close(); err != nil {
		return false, ErrUnavailable
	}
	var state string
	var size int64
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT state,byte_size FROM operation_artifacts WHERE id=?", id).Scan(&state, &size); err != nil {
		return false, ErrUnavailable
	}
	if (state == "ready" || state == "scanning") && int64(count) != (size+SpoolChunkBytes-1)/SpoolChunkBytes {
		missing = true
	}
	return missing, nil
}
