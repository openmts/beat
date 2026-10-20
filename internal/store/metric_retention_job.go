package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

const metricRetentionRetry = time.Minute

func (catalog *MetricCatalogStore) RecoverMetricRetention(ctx context.Context) error {
	_, err := catalog.db.ExecContext(ctx, "UPDATE metric_retention_jobs SET state='queued',next_attempt_at=NULL,last_error='interrupted retention' WHERE state='running'")
	return err
}

func (catalog *MetricCatalogStore) ClaimMetricRetention(ctx context.Context) (*model.MetricRetentionJob, error) {
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := model.NowUTC()
	var job model.MetricRetentionJob
	err = tx.QueryRowContext(ctx, `SELECT j.metric_key,j.revision,j.attempts,
		COALESCE(d.retention_days,(SELECT retention_days FROM maintenance_settings WHERE id=1),30)
		FROM metric_retention_jobs j JOIN metric_definitions d ON d.metric_key=j.metric_key
		WHERE (j.state='queued' AND (j.next_attempt_at IS NULL OR j.next_attempt_at<=?))
		OR (j.state='running' AND j.started_at<?) ORDER BY j.requested_at,j.metric_key LIMIT 1`, now, now.Add(-metriccatalog.RetentionTimeout)).
		Scan(&job.Key, &job.Revision, &job.Attempts, &job.RetentionDays)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.StartedAt = now
	if err := persistMetricRetentionClaim(ctx, tx, job); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func persistMetricRetentionClaim(ctx context.Context, db schemaExecutor, job model.MetricRetentionJob) error {
	result, err := db.ExecContext(ctx, "UPDATE metric_retention_jobs SET state='running',started_at=?,attempts=attempts+1 WHERE metric_key=? AND revision=?", job.StartedAt, job.Key, job.Revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("metric retention claim was not persisted")
	}
	return nil
}

func (catalog *MetricCatalogStore) CompleteMetricRetention(ctx context.Context, result model.MetricRetentionCompletion) error {
	state := "complete"
	var retry *time.Time
	if result.Error != "" {
		state = "queued"
		next := result.CompletedAt.Add(metricRetentionRetry)
		retry = &next
	}
	_, err := catalog.db.ExecContext(ctx, `UPDATE metric_retention_jobs SET state=?,completed_at=?,next_attempt_at=?,last_error=?
		WHERE metric_key=? AND revision=? AND state='running' AND started_at=?`, state, result.CompletedAt, retry, result.Error,
		result.Job.Key, result.Job.Revision, result.Job.StartedAt)
	return err
}
