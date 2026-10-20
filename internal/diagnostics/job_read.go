package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

const maximumJobPage = 200

// JobFilter 是作业列表的固定筛选条件；空值表示不过滤。
type JobFilter struct {
	State      string
	SourceNode string
	TargetID   string
	Limit      int
}

// ListJobs 返回近期作业，按创建时间倒序。
func (service *Service) ListJobs(ctx context.Context, actor Actor, filter JobFilter) ([]model.DiagnosticJob, error) {
	limit := filter.Limit
	if limit <= 0 || limit > maximumJobPage {
		limit = maximumJobPage
	}
	jobs := []model.DiagnosticJob{}
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		rows, err := tx.tx.QueryContext(tx.ctx, `SELECT `+jobColumns+` FROM diagnostic_jobs
			WHERE (?='' OR state=?) AND (?='' OR source_node_id=?) AND (?='' OR target_id=?)
			ORDER BY created_at DESC, id LIMIT ?`, filter.State, filter.State, filter.SourceNode, filter.SourceNode,
			filter.TargetID, filter.TargetID, limit)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		if err := rows.Err(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return jobs, err
}

// JobDetail 是管理员详情：作业元数据加上加密跳身份。
type JobDetail struct {
	Job  model.DiagnosticJob `json:"job"`
	Hops []HopIdentity       `json:"hops"`
}

// Job 返回作业详情；跳地址只在管理员详情中解密返回。
func (service *Service) Job(ctx context.Context, actor Actor, id string) (JobDetail, error) {
	var detail JobDetail
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		job, err := tx.job(id)
		if err != nil {
			return err
		}
		detail.Job = job
		return nil
	})
	if err != nil {
		return JobDetail{}, err
	}
	hops, err := service.hopIdentities(ctx, id)
	if err != nil {
		return JobDetail{}, err
	}
	detail.Hops = hops
	return detail, nil
}

func (tx transaction) job(id string) (model.DiagnosticJob, error) {
	if id == "" {
		return model.DiagnosticJob{}, ErrInvalid
	}
	row := tx.tx.QueryRowContext(tx.ctx, `SELECT `+jobColumns+` FROM diagnostic_jobs WHERE id=?`, id)
	job, err := scanJob(row)
	if errors.Is(err, errNoJob) {
		return model.DiagnosticJob{}, ErrNotFound
	}
	return job, err
}

func scanJob(row rowScanner) (model.DiagnosticJob, error) {
	var job model.DiagnosticJob
	var cancelRequested bool
	var leased, started, completed sql.NullTime
	err := row.Scan(&job.ID, &job.SourceNodeID, &job.TargetID, &job.Kind, &job.State, &job.ActorUserID,
		&job.ActorUsername, &job.Confirmation, &job.SourceNodeName, &job.TargetName, &job.TargetRevision,
		&job.PolicyRevision, &job.Protocol, &job.Direction, &job.IPFamily, &job.Probes, &job.HopLimit,
		&job.DurationSeconds, &job.ParallelStreams, &job.BitrateMbps, &job.PolicySnapshot, &job.TargetSnapshot,
		&job.CapabilityDigest, &job.ReservedBytes, &job.ActualBytes, &job.Attempt, &job.CursorSequence,
		&cancelRequested, &job.ErrorCode, &job.CreatedAt, &job.UpdatedAt, &leased, &started, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DiagnosticJob{}, errNoJob
	}
	if err != nil {
		return model.DiagnosticJob{}, ErrUnavailable
	}
	job.CancelRequested = cancelRequested
	job.LeasedAt = optionalTime(leased)
	job.StartedAt = optionalTime(started)
	job.CompletedAt = optionalTime(completed)
	return job, nil
}

func optionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}

func scanAttempt(row rowScanner) (storedAttempt, error) {
	var attempt storedAttempt
	var heartbeat sql.NullTime
	var started sql.NullTime
	err := row.Scan(&attempt.JobID, &attempt.Attempt, &attempt.State, &attempt.LeaseNonce, &attempt.LeaseExpiresAt,
		&started, &heartbeat)
	if errors.Is(err, sql.ErrNoRows) {
		return storedAttempt{}, ErrNotFound
	}
	if err != nil {
		return storedAttempt{}, ErrUnavailable
	}
	attempt.StartedAt = optionalTime(started)
	if heartbeat.Valid {
		attempt.HeartbeatAt = heartbeat.Time
	}
	return attempt, nil
}
