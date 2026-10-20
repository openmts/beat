package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type storedAttempt struct {
	restored         bool
	kind             string
	attempt          Attempt
	targetID, taskID string
	outputLimit      int
}

func (tx transaction) loadAttempt(request AttemptRequest) (storedAttempt, error) {
	nodeID, err := tx.node(request.Node)
	if err != nil {
		return storedAttempt{}, err
	}
	var item storedAttempt
	var started, heartbeat, completed sql.NullInt64
	var expiry int64
	err = tx.tx.QueryRowContext(tx.ctx, `SELECT a.id,n.node_id,a.state,a.nonce_hash,a.lease_expires_at,a.started_at,a.heartbeat_at,a.completed_at,
		a.result_digest,n.id,t.id,t.output_bytes,COALESCE(json_extract(a.result_json,'$.restore_unknown'),0),t.kind FROM operation_attempts a JOIN operation_task_targets n ON n.id=a.target_id
		JOIN operation_tasks t ON t.id=n.task_id WHERE a.id=? AND n.node_id=?`, request.ID, nodeID).Scan(&item.attempt.ID, &item.attempt.NodeID,
		&item.attempt.State, &item.attempt.NonceHash, &expiry, &started, &heartbeat, &completed, &item.attempt.ResultDigest, &item.targetID, &item.taskID, &item.outputLimit, &item.restored, &item.kind)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, ErrUnavailable
	}
	item.attempt.LeaseExpiresAt = time.UnixMilli(expiry)
	item.attempt.HeartbeatAt = time.UnixMilli(heartbeat.Int64)
	if started.Valid {
		at := time.UnixMilli(started.Int64)
		item.attempt.StartedAt = &at
	}
	if completed.Valid {
		at := time.UnixMilli(completed.Int64)
		item.attempt.CompletedAt = &at
	}
	return item, nil
}

func (service *Service) Start(ctx context.Context, request AttemptRequest) error {
	return service.write(ctx, func(tx transaction) error {
		item, err := tx.loadAttempt(request)
		if err != nil {
			return err
		}
		if service.spool == nil && (item.kind == "deploy" || item.kind == "collect") {
			return ErrUnavailable
		}
		if err := tx.authorizeStart(item, request); err != nil {
			return err
		}
		if err := item.attempt.Start(Proof{NodeID: item.attempt.NodeID, Nonce: request.Nonce, Now: tx.now}); err != nil {
			return err
		}
		if err := tx.persistAttempt(item); err != nil {
			return err
		}
		return tx.audit("", "operation.attempt.started", item.attempt.ID)
	})
}

func (service *Service) Heartbeat(ctx context.Context, request AttemptRequest) (bool, error) {
	var cancel bool
	err := service.atomicWrite(ctx, func(tx transaction) error {
		item, err := tx.loadAttempt(request)
		if err != nil {
			return err
		}
		if item.restored && item.attempt.StartedAt == nil && item.attempt.CompletedAt == nil {
			if err := item.attempt.Authenticate(Proof{NodeID: item.attempt.NodeID, Nonce: request.Nonce, Now: tx.now}); err != nil {
				return err
			}
			item.attempt.HeartbeatAt = tx.now
			cancel = true
			return tx.persistAttempt(item)
		}
		cancel, err = item.attempt.Heartbeat(Proof{NodeID: item.attempt.NodeID, Nonce: request.Nonce, Now: tx.now})
		if err != nil {
			return err
		}
		allowed, err := tx.runningPolicy(item)
		if err != nil {
			return err
		}
		if !allowed || !service.enabled || service.spool == nil && (item.kind == "deploy" || item.kind == "collect") {
			item.attempt.Cancel()
			cancel = true
		}
		return tx.persistAttempt(item)
	})
	return cancel, err
}

func (tx transaction) persistAttempt(item storedAttempt) error {
	var started, completed any
	if item.attempt.StartedAt != nil {
		started = item.attempt.StartedAt.UnixMilli()
	}
	if item.attempt.CompletedAt != nil {
		completed = item.attempt.CompletedAt.UnixMilli()
	}
	if err := tx.one(`UPDATE operation_attempts SET state=?,started_at=?,heartbeat_at=?,completed_at=? WHERE id=?`,
		item.attempt.State, started, item.attempt.HeartbeatAt.UnixMilli(), completed, item.attempt.ID); err != nil {
		return err
	}
	var current string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT current_attempt FROM operation_task_targets WHERE id=?", item.targetID).Scan(&current); err != nil {
		return ErrUnavailable
	}
	if current != item.attempt.ID {
		return nil
	}
	if err := tx.one("UPDATE operation_task_targets SET state=?,updated_at=? WHERE id=? AND current_attempt=?", item.attempt.State, tx.now.UnixMilli(), item.targetID, item.attempt.ID); err != nil {
		return err
	}
	return tx.reflectTransferState(item)
}

func (service *Service) CancelTask(ctx context.Context, actor Actor, id string) error {
	return service.atomicWrite(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		return tx.cancelTask(actor, id)
	})
}

func (tx transaction) cancelTask(actor Actor, id string) error {
	var exists int
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_tasks WHERE id=?", id).Scan(&exists); err != nil {
		return ErrUnavailable
	}
	if exists == 0 {
		return ErrNotFound
	}
	if err := tx.exec("UPDATE operation_tasks SET cancel_requested=1,updated_at=? WHERE id=?", tx.now.UnixMilli(), id); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_attempts SET state=CASE WHEN started_at IS NULL THEN 'canceled' ELSE 'cancel_requested' END
		WHERE completed_at IS NULL AND state IN ('leased','running','unknown','cancel_requested') AND target_id IN
		(SELECT id FROM operation_task_targets WHERE task_id=?)`, id); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_task_targets SET state=CASE WHEN state IN ('queued','leased') THEN 'canceled' ELSE 'cancel_requested' END,
		updated_at=? WHERE task_id=? AND state IN ('queued','leased','running','unknown','cancel_requested')`, tx.now.UnixMilli(), id); err != nil {
		return err
	}
	return tx.audit(actor.UserID, "operation.task.canceled", id)
}
