package store

import (
	"context"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// Cancel 只允许取消仍在排队的任务；实体墓碑在同一事务里恢复，避免实体永久隐藏。
func (store *MetricErasureStore) Cancel(ctx context.Context, id string, now time.Time) error {
	if !model.ValidUUIDShape(id) || now.IsZero() {
		return model.ErrMetricErasureInvalid
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	job, err := queryMetricErasureJob(ctx, tx, "SELECT "+metricErasureColumns+
		" FROM metric_erasure_jobs WHERE id = ?", id)
	if err != nil {
		return err
	}
	if job == nil {
		return model.ErrMetricErasureMissing
	}
	if job.State != model.MetricErasureQueued {
		return model.ErrMetricErasureConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state='canceled', lease_owner='',
		lease_expires_at=NULL, next_attempt_at=NULL, completed_at=? WHERE id=? AND state='queued'`, now, id)
	if err := metricErasureOne(result, err); err != nil {
		return err
	}
	if err := restoreMetricErasureEntity(ctx, tx, *job, now); err != nil {
		return err
	}
	if err := mutation.finish(metricErasureAdminEvidence(*job, "metric_erasure.cancel")); err != nil {
		return err
	}
	return tx.Commit()
}

// Retry 让失败任务重新排队；游标保留，因此只会重做尚未完成的部分。
func (store *MetricErasureStore) Retry(ctx context.Context, id string, now time.Time) error {
	if !model.ValidUUIDShape(id) || now.IsZero() {
		return model.ErrMetricErasureInvalid
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	job, err := queryMetricErasureJob(ctx, tx, "SELECT "+metricErasureColumns+
		" FROM metric_erasure_jobs WHERE id = ?", id)
	if err != nil {
		return err
	}
	if job == nil {
		return model.ErrMetricErasureMissing
	}
	if job.State != model.MetricErasureFailed {
		return model.ErrMetricErasureConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE metric_erasure_jobs SET state='queued', attempt_count=0,
		lease_owner='', lease_expires_at=NULL, next_attempt_at=NULL, last_error_code='', completed_at=NULL
		WHERE id=? AND state='failed'`, id)
	if err := metricErasureOne(result, err); err != nil {
		return err
	}
	if err := mutation.finish(metricErasureAdminEvidence(*job, "metric_erasure.retry")); err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizeEntity 删除已擦除实体的应用行；重复调用视为已收尾。
func (store *MetricErasureStore) FinalizeEntity(
	ctx context.Context,
	job model.MetricErasureJob,
	now time.Time,
) error {
	if !job.ErasesEntity() || job.TargetID == "" || now.IsZero() {
		return nil
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if job.Scope == model.MetricErasureDeleteNode {
		if err := guardOperationNodeDelete(ctx, tx, job.TargetID); err != nil {
			return err
		}
		if err := deleteMetricErasureRow(ctx, tx, "DELETE FROM nodes WHERE id = ?", job.TargetID); err != nil {
			return err
		}
	} else if err := deleteMetricErasureRow(ctx, tx, "DELETE FROM network_tasks WHERE id = ?", job.TargetID); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteMetricErasureRow(ctx context.Context, tx schemaExecutor, query, id string) error {
	if _, err := tx.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("finalize metric erasure entity: %w", err)
	}
	return nil
}

// restoreMetricErasureEntity 撤销取消任务的实体墓碑。
func restoreMetricErasureEntity(
	ctx context.Context,
	tx schemaExecutor,
	job model.MetricErasureJob,
	now time.Time,
) error {
	var query string
	switch job.Scope {
	case model.MetricErasureDeleteNode:
		query = "UPDATE nodes SET lifecycle_state = 'active', updated_at = ? WHERE id = ? AND lifecycle_state = 'deleting'"
	case model.MetricErasureDeleteNetworkTask:
		query = "UPDATE network_tasks SET lifecycle_state = 'active', updated_at = ? WHERE id = ? AND lifecycle_state = 'deleting'"
	default:
		return nil
	}
	if _, err := tx.ExecContext(ctx, query, now, job.TargetID); err != nil {
		return fmt.Errorf("restore metric erasure entity: %w", err)
	}
	return nil
}

func metricErasureAdminEvidence(job model.MetricErasureJob, action string) metadataEvidence {
	return metadataEvidence{Action: action, Type: "metric_erasure", ID: job.ID,
		Name: job.TargetName, Fields: []string{"scope", "state"}}
}
