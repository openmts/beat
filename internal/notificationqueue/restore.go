package notificationqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// ReconcileRestore 只改私有待激活快照；网络派发尚未启动。
func ReconcileRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	database, err := sql.Open("sqlite", uri+"?mode=rw&_txlock=immediate&_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var present int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name='notification_deliveries'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var leases int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased' AND lease_expires_at<=?", now.UnixMilli()).Scan(&leases); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET outcome='lease_expired',completed_at=?,duration_ms=MAX(0,?-started_at),error_code='restore_recovery',error_stage='recovery'
		WHERE outcome='in_progress' AND delivery_id IN(SELECT id FROM notification_deliveries WHERE state='leased' AND lease_expires_at<=?)`, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state=CASE WHEN attempt_count>=6 THEN 'dead' ELSE 'pending' END,
		lease_token_hash=NULL,lease_expires_at=NULL,worker_id='',next_attempt_at=?,updated_at=?,completed_at=CASE WHEN attempt_count>=6 THEN ? ELSE NULL END,
		error_code='restore_recovery',error_stage='recovery' WHERE state='leased' AND lease_expires_at<=?`, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return err
	}
	id, err := uuid.NewRandomFromReader(rand.Reader)
	if err != nil {
		return err
	}
	detail, err := json.Marshal(map[string]int{"recovered_leases": leases})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
		VALUES(?,?,NULL,'system','notification.restore.reconcile','notification_queue','restore','success',?,'','','',?)`, id.String(), id.String(), string(detail), now)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
