package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

const metricErasureColumns = `id,scope,target_type,target_id,target_name,state,cutoff_unix_nano,
	catalog_digest,measurement_cursor,attempt_count,lease_owner,lease_expires_at,next_attempt_at,
	request_id,actor_id,actor_name,session_prefix,last_error_code,requested_at,started_at,completed_at`

// MaximumMetricErasureRows 限制单次列表返回的任务条数。
const MaximumMetricErasureRows = 100

func scanMetricErasureJob(row rowScanner) (model.MetricErasureJob, error) {
	var job model.MetricErasureJob
	var cutoff sql.NullInt64
	var lease, next, started, completed sql.NullTime
	err := row.Scan(&job.ID, &job.Scope, &job.TargetType, &job.TargetID, &job.TargetName, &job.State,
		&cutoff, &job.CatalogDigest, &job.MeasurementCursor, &job.AttemptCount, &job.LeaseOwner,
		&lease, &next, &job.RequestID, &job.ActorID, &job.ActorName, &job.SessionPrefix,
		&job.LastErrorCode, &job.RequestedAt, &started, &completed)
	if err != nil {
		return model.MetricErasureJob{}, err
	}
	job.CutoffUnixNano = optionalInt64(cutoff)
	job.LeaseExpiresAt = optionalTime(lease, true)
	job.NextAttemptAt = optionalTime(next, true)
	job.StartedAt = optionalTime(started, true)
	job.CompletedAt = optionalTime(completed, true)
	job.RequestedAt = job.RequestedAt.UTC()
	return job, nil
}

func queryMetricErasureJob(
	ctx context.Context,
	executor schemaExecutor,
	query string,
	args ...any,
) (*model.MetricErasureJob, error) {
	job, err := scanMetricErasureJob(executor.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read metric erasure job: %w", err)
	}
	return &job, nil
}

// Get 读取单个任务；不存在时返回 nil。
func (store *MetricErasureStore) Get(ctx context.Context, id string) (*model.MetricErasureJob, error) {
	if !model.ValidUUIDShape(id) {
		return nil, nil
	}
	return queryMetricErasureJob(ctx, store.db,
		"SELECT "+metricErasureColumns+" FROM metric_erasure_jobs WHERE id = ?", id)
}

// List 返回最近任务，活动任务与历史任务使用同一顺序，便于前端只做一次渲染。
func (store *MetricErasureStore) List(ctx context.Context, limit int) ([]model.MetricErasureJob, error) {
	if limit <= 0 || limit > MaximumMetricErasureRows {
		limit = 25
	}
	rows, err := store.db.QueryContext(ctx, "SELECT "+metricErasureColumns+
		" FROM metric_erasure_jobs ORDER BY requested_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list metric erasure jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	jobs := []model.MetricErasureJob{}
	for rows.Next() {
		job, err := scanMetricErasureJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan metric erasure job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metric erasure jobs: %w", err)
	}
	return jobs, nil
}

func findMetricErasureByRequest(
	ctx context.Context,
	executor schemaExecutor,
	requestID string,
) (*model.MetricErasureJob, bool, error) {
	job, err := queryMetricErasureJob(ctx, executor,
		"SELECT "+metricErasureColumns+" FROM metric_erasure_jobs WHERE request_id = ?", requestID)
	if err != nil || job == nil {
		return nil, false, err
	}
	return job, true, nil
}

// sameMetricErasureRequest 判断重放是否完全一致；任何字段差异都是冲突。
func sameMetricErasureRequest(job model.MetricErasureJob, request model.MetricErasureCreateRequest) bool {
	return job.Scope == request.Scope && job.TargetID == request.TargetID &&
		job.TargetName == request.TargetName &&
		job.TargetType == model.MetricErasureTargetTypeFor(request.Scope)
}

// Status 汇总耐久工作状态，供就绪探针与指标区分“正在工作”和“需要人工处理”。
func (store *MetricErasureStore) Status(ctx context.Context) (MetricErasureStatus, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT state, scope, COUNT(*) FROM metric_erasure_jobs
		GROUP BY state, scope`)
	if err != nil {
		return MetricErasureStatus{}, fmt.Errorf("read metric erasure status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	status := MetricErasureStatus{counts: map[string]int{}, activeScopes: map[string]int{}}
	for rows.Next() {
		var state, scope string
		var count int
		if err := rows.Scan(&state, &scope, &count); err != nil {
			return MetricErasureStatus{}, fmt.Errorf("scan metric erasure status: %w", err)
		}
		status.counts[state] += count
		if activeMetricErasureState(state) {
			status.activeScopes[scope] += count
		}
	}
	if err := rows.Err(); err != nil {
		return MetricErasureStatus{}, fmt.Errorf("iterate metric erasure status: %w", err)
	}
	return status, nil
}

// MetricErasureStates 返回全部状态，顺序固定便于指标输出稳定。
func MetricErasureStates() []string {
	return []string{model.MetricErasureQueued, model.MetricErasureClaimed, model.MetricErasureDeleting,
		model.MetricErasureCompacting, model.MetricErasureFinalizing, model.MetricErasureSucceeded,
		model.MetricErasureFailed, model.MetricErasureCanceled}
}

func activeMetricErasureState(state string) bool {
	for _, candidate := range MetricErasureStates()[:5] {
		if candidate == state {
			return true
		}
	}
	return false
}

// MetricErasureStatus 是就绪探针与指标需要的计数快照。
type MetricErasureStatus struct {
	counts       map[string]int
	activeScopes map[string]int
}

// Counts 返回按状态聚合的副本。
func (status MetricErasureStatus) Counts() map[string]int {
	copied := make(map[string]int, len(status.counts))
	for state, count := range status.counts {
		copied[state] = count
	}
	return copied
}

// Active 报告是否仍有未完成的擦除任务。
func (status MetricErasureStatus) Active() bool {
	for _, state := range MetricErasureStates()[:5] {
		if status.counts[state] > 0 {
			return true
		}
	}
	return false
}

// Degraded 报告是否存在需要人工重试的失败任务。
func (status MetricErasureStatus) Degraded() bool {
	return status.counts[model.MetricErasureFailed] > 0
}

// MetricErasureScopeCount 是一个活动范围的任务数。
type MetricErasureScopeCount struct {
	Name  string
	Count int
}

// MetricErasureActiveScopes 返回按范围聚合的活动任务数，顺序固定。
func MetricErasureActiveScopes(status MetricErasureStatus) []MetricErasureScopeCount {
	scopes := model.MetricErasureScopes()
	counts := make([]MetricErasureScopeCount, 0, len(scopes))
	for _, scope := range scopes {
		counts = append(counts, MetricErasureScopeCount{Name: scope, Count: status.activeScopes[scope]})
	}
	return counts
}
