package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// ReconcileMetricErasureRestore 把恢复出来的在途擦除任务重新排队。
//
// 备份可能是在任务运行中被捕获的：leased/claimed/deleting/compacting/finalizing 都不
// 代表已经完成，因此恢复后统一回到 queued 并清空租约，让 worker 从已提交的游标继续。
// 已经产生 tombstone 的测量会被重新删除，删除本身是幂等的。
func ReconcileMetricErasureRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_txlock=immediate&_pragma=foreign_keys(1)"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var present int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE name='metric_erasure_jobs'").Scan(&present); err != nil {
		return fmt.Errorf("inspect restored metric erasure ledger: %w", err)
	}
	if present == 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state='queued', lease_owner='',
		lease_expires_at=NULL, next_attempt_at=NULL WHERE state IN
		('claimed','deleting','compacting','finalizing')`); err != nil {
		return fmt.Errorf("requeue restored metric erasure jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state='failed', completed_at=?,
		last_error_code=? WHERE state='queued' AND attempt_count >= ?`,
		now, "lease_expired", MaximumMetricErasureAttempts); err != nil {
		return fmt.Errorf("fail restored metric erasure jobs: %w", err)
	}
	return tx.Commit()
}
