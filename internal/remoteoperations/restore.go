package remoteoperations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// ReconcileRestore 只操作尚未激活的私有副本；备份之后是否执行过不能由旧快照证明。
func ReconcileRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	database, err := sql.Open("sqlite", uri+"?mode=rw&_txlock=immediate&_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var present int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='operation_tasks'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	db, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = db.Rollback() }()
	tx := transaction{ctx: ctx, tx: db, now: now, authority: &Actor{Username: "system"}}
	if err := tx.reconcileRestoredOperations(); err != nil {
		return err
	}
	return db.Commit()
}

func (tx transaction) reconcileRestoredOperations() error {
	var unfinished int
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_task_targets WHERE state IN ('queued','leased','running','unknown','cancel_requested')").Scan(&unfinished); err != nil {
		return ErrUnavailable
	}
	if err := tx.exec(`UPDATE operation_tasks SET policy_snapshot=json_set(policy_snapshot,'$.restore_risk',json('true')),
 cancel_requested=CASE WHEN EXISTS(SELECT 1 FROM operation_task_targets WHERE task_id=operation_tasks.id AND state IN ('queued','leased','running','unknown','cancel_requested')) THEN 1 ELSE cancel_requested END`); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_attempts SET state='unknown',result_json=json_set(result_json,'$.restore_unknown',json('true'))
 WHERE completed_at IS NULL AND state IN ('leased','running','unknown','cancel_requested')`); err != nil {
		return err
	}
	if err := tx.exec("UPDATE operation_task_targets SET state='unknown',updated_at=? WHERE state IN ('queued','leased','running','unknown','cancel_requested')", tx.now.UnixMilli()); err != nil {
		return err
	}
	if err := tx.exec("UPDATE operation_transfer_targets SET state='unknown',updated_at=? WHERE state IN ('queued','leased','running','unknown','cancel_requested','uploading')", tx.now.UnixMilli()); err != nil {
		return err
	}
	if err := tx.exec("UPDATE operation_artifacts SET state='expired',scan_id='',scan_deadline=0,expires_at=?,updated_at=?", tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return err
	}
	detail, err := json.Marshal(map[string]int{"unknown_targets": unfinished})
	if err != nil {
		return ErrUnavailable
	}
	return tx.one(`INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
 VALUES(?,?,NULL,'system','operation.restore.reconciled','operation','restore','success',?,'','','',?)`, uuid.NewString(), uuid.NewString(), string(detail), tx.now)
}
