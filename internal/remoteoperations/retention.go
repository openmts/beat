package remoteoperations

import (
	"errors"
	"time"
)

const operationTaskRetention = 30 * 24 * time.Hour

const taskRetentionDaysSQL = `CASE WHEN CAST(json_extract(t.policy_snapshot,'$.retention.task_days') AS INTEGER) BETWEEN 1 AND 90 THEN CAST(json_extract(t.policy_snapshot,'$.retention.task_days') AS INTEGER) ELSE 30 END`

const outputRetentionDaysSQL = `MIN((` + taskRetentionDaysSQL + `),CASE WHEN CAST(json_extract(t.policy_snapshot,'$.retention.output_days') AS INTEGER) BETWEEN 1 AND 30 THEN CAST(json_extract(t.policy_snapshot,'$.retention.output_days') AS INTEGER) ELSE 7 END)`

func (tx transaction) retainOperations() error {
	if err := tx.exec(`UPDATE operation_attempts SET stdout_encrypted=NULL,stderr_encrypted=NULL WHERE id IN (
 SELECT a.id FROM operation_attempts a JOIN operation_task_targets n ON n.id=a.target_id JOIN operation_tasks t ON t.id=n.task_id
 WHERE a.completed_at IS NOT NULL AND a.completed_at < ?-(`+outputRetentionDaysSQL+`)*86400000
 AND (a.stdout_encrypted IS NOT NULL OR a.stderr_encrypted IS NOT NULL) LIMIT 64)`, tx.now.UnixMilli()); err != nil {
		return err
	}
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT t.id FROM operation_tasks t JOIN operation_task_targets n ON n.task_id=t.id
 WHERE NOT EXISTS(SELECT 1 FROM operation_transfers f WHERE f.task_id=t.id AND f.artifact_id IS NOT NULL)
 GROUP BY t.id HAVING MAX(n.updated_at)<?-(`+taskRetentionDaysSQL+`)*86400000 AND SUM(n.node_id IS NOT NULL AND n.state IN ('queued','leased','running','unknown','cancel_requested'))=0 LIMIT 8`, tx.now.UnixMilli())
	if err != nil {
		return ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return ErrUnavailable
	}
	for _, id := range ids {
		if err := tx.deleteRetainedTask(id); err != nil {
			return err
		}
	}
	return tx.retainTransfers()
}

func (tx transaction) deleteRetainedTask(id string) error {
	if err := tx.exec("UPDATE operation_transfers SET task_id=NULL WHERE task_id=?", id); err != nil {
		return err
	}
	if err := tx.exec("DELETE FROM operation_attempts WHERE target_id IN (SELECT id FROM operation_task_targets WHERE task_id=?)", id); err != nil {
		return err
	}
	if err := tx.exec("DELETE FROM operation_task_targets WHERE task_id=?", id); err != nil {
		return err
	}
	if err := tx.one("DELETE FROM operation_tasks WHERE id=?", id); err != nil {
		return err
	}
	return tx.audit("", "operation.task.retention_expired", id)
}

func (tx transaction) retainTransfers() error {
	const expired = `SELECT id FROM operation_transfers WHERE artifact_id IS NULL AND task_id IS NULL AND updated_at<? LIMIT 64`
	cutoff := tx.now.Add(-operationTaskRetention).UnixMilli()
	if err := tx.exec("DELETE FROM operation_transfer_targets WHERE transfer_id IN ("+expired+")", cutoff); err != nil {
		return err
	}
	return tx.exec("DELETE FROM operation_transfers WHERE id IN ("+expired+")", cutoff)
}
