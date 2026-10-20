package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// 租约与重试边界：擦除是不可逆操作，失败必须可见且可重试，绝不静默丢弃任务。
const (
	MetricErasureLeaseLifetime   = 2 * time.Minute
	MaximumMetricErasureAttempts = 6
	maximumMetricErasureBackoff  = 5 * time.Minute
)

// MetricErasureClaim 是一次带租约的领取；ExpiresAt 之后任务可以被其它 worker 接管。
type MetricErasureClaim struct {
	Job       model.MetricErasureJob
	ExpiresAt time.Time
}

func metricErasureBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return time.Second
	}
	if attempts > 20 {
		attempts = 20
	}
	delay := time.Duration(1<<uint(attempts-1)) * time.Second
	if delay > maximumMetricErasureBackoff {
		return maximumMetricErasureBackoff
	}
	return delay
}

// Claim 在事务里回收过期租约并领取一个到期任务；没有可领取任务时返回 nil。
func (store *MetricErasureStore) Claim(
	ctx context.Context,
	workerID string,
	now time.Time,
) (*MetricErasureClaim, error) {
	if workerID == "" || now.IsZero() {
		return nil, model.ErrMetricErasureInvalid
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recoverMetricErasureExpired(ctx, tx, now); err != nil {
		return nil, err
	}
	job, err := queryMetricErasureJob(ctx, tx, "SELECT "+metricErasureColumns+` FROM metric_erasure_jobs
		WHERE state = 'queued' AND attempt_count < ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY requested_at, id LIMIT 1`, MaximumMetricErasureAttempts, now)
	if err != nil {
		return nil, err
	}
	if job == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	expires := now.Add(MetricErasureLeaseLifetime)
	result, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state='claimed', attempt_count=?,
		lease_owner=?, lease_expires_at=?, next_attempt_at=NULL, started_at=COALESCE(started_at,?)
		WHERE id=? AND state='queued' AND attempt_count=?`, job.AttemptCount+1, workerID, expires,
		now, job.ID, job.AttemptCount)
	if err := metricErasureOne(result, err); err != nil {
		return nil, err
	}
	job.State, job.AttemptCount, job.LeaseOwner, job.LeaseExpiresAt = model.MetricErasureClaimed,
		job.AttemptCount+1, workerID, &expires
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MetricErasureClaim{Job: *job, ExpiresAt: expires}, nil
}

func recoverMetricErasureExpired(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,attempt_count FROM metric_erasure_jobs
		WHERE state IN ('claimed','deleting','compacting','finalizing') AND lease_expires_at <= ?`, now)
	if err != nil {
		return fmt.Errorf("read expired metric erasure leases: %w", err)
	}
	type expired struct {
		id       string
		attempts int
	}
	pending := []expired{}
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.id, &item.attempts); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan expired metric erasure lease: %w", err)
		}
		pending = append(pending, item)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("read expired metric erasure leases: %w", err)
	}
	for _, item := range pending {
		state, code := model.MetricErasureQueued, ""
		var completed any
		next := now.Add(metricErasureBackoff(item.attempts))
		if item.attempts >= MaximumMetricErasureAttempts {
			state, code, completed = model.MetricErasureFailed, model.MetricErasureErrorLease, now
		}
		result, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state=?, lease_owner='',
			lease_expires_at=NULL, next_attempt_at=?, last_error_code=?, completed_at=?
			WHERE id=? AND state IN ('claimed','deleting','compacting','finalizing')`, state, next, code,
			completed, item.id)
		if err := metricErasureOne(result, err); err != nil {
			return err
		}
	}
	return nil
}

// BeginDeleting 记录冻结的截止时间与测量清单摘要，并把任务推进到删除阶段。
func (store *MetricErasureStore) BeginDeleting(
	ctx context.Context,
	claim MetricErasureClaim,
	digest string,
	cutoffUnixNano int64,
	now time.Time,
) error {
	if !model.ValidMetricErasureDigest(digest) || cutoffUnixNano <= 0 {
		return model.ErrMetricErasureInvalid
	}
	return store.transition(ctx, transitionInput{id: claim.Job.ID, from: []string{model.MetricErasureClaimed},
		state: model.MetricErasureDeleting, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='deleting', catalog_digest=?, cutoff_unix_nano=?,
			measurement_cursor='', lease_expires_at=? WHERE id=? AND state='claimed'`,
		args: []any{digest, cutoffUnixNano, now.Add(MetricErasureLeaseLifetime), claim.Job.ID}})
}

// Advance 只在一次测量删除成功后推进游标；它是断点续跑的唯一进度来源。
func (store *MetricErasureStore) Advance(
	ctx context.Context,
	id string,
	cursor string,
	now time.Time,
) error {
	return store.transition(ctx, transitionInput{id: id, from: []string{model.MetricErasureDeleting},
		state: model.MetricErasureDeleting, now: now,
		statement: `UPDATE metric_erasure_jobs SET measurement_cursor=?, lease_expires_at=?
			WHERE id=? AND state='deleting'`, args: []any{cursor, now.Add(MetricErasureLeaseLifetime), id}})
}

// BeginCompacting 表示全部 tombstone 已发布；之后的重试只会重做压缩与收尾。
func (store *MetricErasureStore) BeginCompacting(ctx context.Context, id string, now time.Time) error {
	return store.transition(ctx, transitionInput{id: id, from: []string{model.MetricErasureDeleting},
		state: model.MetricErasureCompacting, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='compacting', lease_expires_at=?
			WHERE id=? AND state='deleting'`, args: []any{now.Add(MetricErasureLeaseLifetime), id}})
}

// BeginFinalizing 进入收尾阶段：先删除仍需删除的实体应用行，再标记成功。
func (store *MetricErasureStore) BeginFinalizing(ctx context.Context, id string, now time.Time) error {
	return store.transition(ctx, transitionInput{id: id, from: []string{model.MetricErasureCompacting},
		state: model.MetricErasureFinalizing, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='finalizing', lease_expires_at=?
			WHERE id=? AND state='compacting'`, args: []any{now.Add(MetricErasureLeaseLifetime), id}})
}

// Complete 写入终态成功并清空租约。
func (store *MetricErasureStore) Complete(ctx context.Context, id string, now time.Time) error {
	return store.transition(ctx, transitionInput{id: id, from: []string{model.MetricErasureFinalizing},
		state: model.MetricErasureSucceeded, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='succeeded', lease_owner='', lease_expires_at=NULL,
			next_attempt_at=NULL, last_error_code='', completed_at=? WHERE id=? AND state='finalizing'`,
		args: []any{now, id}})
}

// Fail 记录脱敏错误码并进入失败终态；失败任务保持可见、可重试。
func (store *MetricErasureStore) Fail(ctx context.Context, id string, code string, now time.Time) error {
	if code == "" || len(code) > model.MaximumMetricErasureErrorBytes {
		code = model.MetricErasureErrorInternal
	}
	from := []string{model.MetricErasureClaimed, model.MetricErasureDeleting,
		model.MetricErasureCompacting, model.MetricErasureFinalizing}
	return store.transition(ctx, transitionInput{id: id, from: from, state: model.MetricErasureFailed, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='failed', lease_owner='', lease_expires_at=NULL,
			last_error_code=?, completed_at=? WHERE id=? AND state IN ('claimed','deleting','compacting','finalizing')`,
		args: []any{code, now, id}})
}

// Release 在进程退出或租约丢失时把任务交回队列，游标保持不变。
func (store *MetricErasureStore) Release(ctx context.Context, id string, now time.Time) error {
	from := []string{model.MetricErasureClaimed, model.MetricErasureDeleting,
		model.MetricErasureCompacting, model.MetricErasureFinalizing}
	return store.transition(ctx, transitionInput{id: id, from: from, state: model.MetricErasureQueued, now: now,
		statement: `UPDATE metric_erasure_jobs SET state='queued', lease_owner='', lease_expires_at=NULL,
			next_attempt_at=NULL WHERE id=? AND state IN ('claimed','deleting','compacting','finalizing')`,
		args: []any{id}})
}

type transitionInput struct {
	id, state string
	from      []string
	statement string
	args      []any
	now       time.Time
}

func (store *MetricErasureStore) transition(ctx context.Context, input transitionInput) error {
	if input.id == "" || input.now.IsZero() {
		return model.ErrMetricErasureInvalid
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, input.statement, input.args...)
	if err := metricErasureOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

func metricErasureOne(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("update metric erasure job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read metric erasure update count: %w", err)
	}
	if affected != 1 {
		return model.ErrMetricErasureConflict
	}
	return nil
}
