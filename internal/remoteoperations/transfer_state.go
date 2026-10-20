package remoteoperations

import (
	"database/sql"
	"errors"
)

func (tx transaction) validateTransferCompletion(item storedAttempt, result Result) error {
	if result.State != "succeeded" {
		return nil
	}
	var direction, state string
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT t.direction,COALESCE(a.state,'') FROM operation_transfers t LEFT JOIN operation_artifacts a ON a.id=t.artifact_id WHERE t.task_id=?`, item.taskID).Scan(&direction, &state)
	if errors.Is(err, sql.ErrNoRows) {
		if item.kind == "deploy" || item.kind == "collect" {
			return ErrUnavailable
		}
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	if direction == "collect" && state != "ready" {
		return ErrConflict
	}
	return nil
}

func (tx transaction) reflectTransferState(item storedAttempt) error {
	if err := tx.exec(`UPDATE operation_transfer_targets SET state=?,attempt_id=?,updated_at=?,
		progress_bytes=CASE WHEN ?='succeeded' THEN (SELECT byte_size FROM operation_transfers WHERE id=transfer_id) ELSE progress_bytes END
		WHERE transfer_id IN (SELECT id FROM operation_transfers WHERE task_id=?) AND node_id=?`, item.attempt.State, item.attempt.ID, tx.now.UnixMilli(), item.attempt.State, item.taskID, item.attempt.NodeID); err != nil {
		return err
	}
	return tx.exec(`UPDATE operation_transfers SET state=CASE
		WHEN EXISTS(SELECT 1 FROM operation_task_targets WHERE task_id=? AND state IN ('queued','leased','running','unknown','cancel_requested')) THEN 'running'
		WHEN EXISTS(SELECT 1 FROM operation_task_targets WHERE task_id=? AND state<>'succeeded') THEN 'failed' ELSE 'succeeded' END,
		updated_at=? WHERE task_id=?`, item.taskID, item.taskID, tx.now.UnixMilli(), item.taskID)
}

func (tx transaction) transferQuota(creation *transferCreation, spool *Spool) error {
	const maximumArtifacts = 256
	const personalBytes int64 = 1024 * 1024 * 1024
	var count int
	var reserved int64
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*),COALESCE(SUM(byte_size+((byte_size+4194303)/4194304)*128),0) FROM operation_artifacts").Scan(&count, &reserved); err != nil {
		return ErrUnavailable
	}
	if count >= maximumArtifacts && !creation.existing || reserved > spool.maximumBytes-spoolReservation(creation.input.File.Size) {
		return ErrForbidden
	}
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COALESCE(SUM(byte_size+((byte_size+4194303)/4194304)*128),0) FROM operation_artifacts WHERE actor_id=?", creation.actorID).Scan(&reserved); err != nil {
		return ErrUnavailable
	}
	if reserved > min(personalBytes, spool.maximumBytes)-spoolReservation(creation.input.File.Size) {
		return ErrForbidden
	}
	for _, node := range creation.nodes {
		if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COALESCE(SUM(a.byte_size+((a.byte_size+4194303)/4194304)*128),0) FROM operation_artifacts a JOIN operation_transfers f ON f.artifact_id=a.id
			JOIN operation_transfer_targets n ON n.transfer_id=f.id WHERE n.node_id=?`, node.id).Scan(&reserved); err != nil {
			return ErrUnavailable
		}
		if reserved > min(personalBytes, spool.maximumBytes)-spoolReservation(creation.input.File.Size) {
			return ErrForbidden
		}
	}
	return nil
}

func spoolReservation(size int64) int64 { return size + ((size+SpoolChunkBytes-1)/SpoolChunkBytes)*128 }
