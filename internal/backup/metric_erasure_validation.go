package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

const (
	metricErasureSchemaVersion = 22
	maximumMetricErasureRows   = 1_000_000
)

// validateMetricErasureLedger 校验归档快照里的耐久擦除账本。
//
// 返回 handled=false 表示这份备份来自更早的能力版本，恢复方按“没有擦除任务”处理。
// 返回 handled=true 时任何不一致都必须让恢复失败：账本决定哪些实体会被收尾删除、
// 哪些 MTS 序列已经被擦掉，损坏的账本可能让已请求删除的实体重新可见。
func validateMetricErasureLedger(ctx context.Context, sqlitePath string) (handled bool, resultErr error) {
	database, err := openArchivedSQLite(sqlitePath)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	version, err := archivedSchemaVersion(ctx, database)
	if err != nil {
		return false, err
	}
	if version < metricErasureSchemaVersion {
		return false, nil
	}
	jobs, err := archivedMetricErasureJobs(ctx, database)
	if err != nil {
		return true, err
	}
	nodes, err := archivedNodeIDs(ctx, database)
	if err != nil {
		return true, err
	}
	tasks, err := archivedNetworkTaskIDs(ctx, database)
	if err != nil {
		return true, err
	}
	if err := validateMetricErasureJobs(jobs, nodes, tasks); err != nil {
		return true, err
	}
	return true, nil
}

func archivedNetworkTaskIDs(ctx context.Context, database *sql.DB) (map[string]bool, error) {
	rows, err := database.QueryContext(ctx, "SELECT id FROM network_tasks")
	if err != nil {
		if isNoSuchTable(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("read archived network tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tasks := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan archived network task: %w", err)
		}
		tasks[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read archived network tasks: %w", err)
	}
	return tasks, nil
}

func archivedMetricErasureJobs(ctx context.Context, database *sql.DB) ([]model.MetricErasureJob, error) {
	rows, err := database.QueryContext(ctx, `SELECT id,scope,target_type,target_id,target_name,state,
		cutoff_unix_nano,measurement_cursor,attempt_count,last_error_code,request_id,requested_at,
		completed_at FROM metric_erasure_jobs ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("read archived metric erasure ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	jobs := make([]model.MetricErasureJob, 0)
	for rows.Next() {
		if len(jobs) >= maximumMetricErasureRows {
			return nil, errors.New("archived metric erasure ledger exceeds the row limit")
		}
		job, err := scanArchivedMetricErasureJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read archived metric erasure ledger: %w", err)
	}
	return jobs, nil
}

func scanArchivedMetricErasureJob(row calibrationScanner) (model.MetricErasureJob, error) {
	var job model.MetricErasureJob
	var cutoff sql.NullInt64
	var completed sql.NullTime
	err := row.Scan(&job.ID, &job.Scope, &job.TargetType, &job.TargetID, &job.TargetName, &job.State,
		&cutoff, &job.MeasurementCursor, &job.AttemptCount, &job.LastErrorCode, &job.RequestID,
		&job.RequestedAt, &completed)
	if err != nil {
		return model.MetricErasureJob{}, fmt.Errorf("scan archived metric erasure job: %w", err)
	}
	if cutoff.Valid {
		value := cutoff.Int64
		job.CutoffUnixNano = &value
	}
	job.CompletedAt = archivedOptionalTime(completed)
	job.RequestedAt = job.RequestedAt.UTC()
	return job, nil
}

// validateMetricErasureJobs 校验每一行的形状、实体关联与活动范围唯一性。
func validateMetricErasureJobs(jobs []model.MetricErasureJob, nodes, tasks map[string]bool) error {
	activeTargets := map[string]bool{}
	activeFleet := map[string]bool{}
	for _, job := range jobs {
		if err := validateArchivedMetricErasureJob(job, nodes, tasks); err != nil {
			return err
		}
		if !job.Active() {
			continue
		}
		key := job.TargetType + "\x00" + job.TargetID
		if job.TargetID == "" {
			if activeFleet[job.Scope] {
				return errors.New("archived metric erasure ledger has two active fleet-wide jobs")
			}
			activeFleet[job.Scope] = true
			continue
		}
		if activeTargets[key] {
			return errors.New("archived metric erasure ledger has two active jobs for one entity")
		}
		activeTargets[key] = true
	}
	return nil
}
