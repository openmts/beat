package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// MetricErasureStore 是耐久擦除任务账本的唯一读写入口。
type MetricErasureStore struct {
	db *sql.DB
}

func NewMetricErasureStore(db *sql.DB) *MetricErasureStore {
	return &MetricErasureStore{db: db}
}

// MetricErasureCreate 是一次显式擦除请求；确认值与显示名由服务端在事务内解析。
type MetricErasureCreate struct {
	Request model.MetricErasureCreateRequest
	Now     time.Time
}

// Create 在同一个 SQLite 事务里解析目标、标记实体、追加任务并写审计。
//
// 解析显示名的动作放在事务内，是为了让“确认短语”永远对应服务端读取到的名字：
// 调用方无法用一次查询与一次写入之间的改名竞态伪造确认值。
func (store *MetricErasureStore) Create(
	ctx context.Context,
	input MetricErasureCreate,
) (model.MetricErasureJob, bool, error) {
	if input.Now.IsZero() || !model.ValidMetricErasureScope(input.Request.Scope) ||
		!model.ValidUUIDShape(input.Request.RequestID) {
		return model.MetricErasureJob{}, false, model.ErrMetricErasureInvalid
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return model.MetricErasureJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return model.MetricErasureJob{}, false, err
	}
	request := input.Request
	if err := resolveMetricErasureTarget(ctx, tx, &request); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	if err := request.Validate(); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	replay, found, err := findMetricErasureByRequest(ctx, tx, request.RequestID)
	if err != nil {
		return model.MetricErasureJob{}, false, err
	}
	if found {
		if !sameMetricErasureRequest(*replay, request) {
			return model.MetricErasureJob{}, false, model.ErrMetricErasureConflict
		}
		if err := tx.Commit(); err != nil {
			return model.MetricErasureJob{}, false, err
		}
		return *replay, true, nil
	}
	if err := guardMetricErasureConcurrency(ctx, tx, request); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	job := metricErasureJobFromRequest(request, input.Now)
	if err := tombstoneMetricErasureEntity(ctx, tx, request, input.Now); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	if err := insertMetricErasureJob(ctx, tx, job); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	if err := mutation.finish(metricErasureEvidence(job)); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.MetricErasureJob{}, false, err
	}
	return job, false, nil
}

func metricErasureJobFromRequest(request model.MetricErasureCreateRequest, now time.Time) model.MetricErasureJob {
	return model.MetricErasureJob{ID: uuid.NewString(), Scope: request.Scope,
		TargetType: model.MetricErasureTargetTypeFor(request.Scope), TargetID: request.TargetID,
		TargetName: request.TargetName, State: model.MetricErasureQueued, RequestID: request.RequestID,
		RequestedAt: now}
}

// resolveMetricErasureTarget 读取实体显示名；舰队级范围不需要目标。
func resolveMetricErasureTarget(
	ctx context.Context,
	tx *sql.Tx,
	request *model.MetricErasureCreateRequest,
) error {
	switch model.MetricErasureTargetTypeFor(request.Scope) {
	case model.MetricErasureTargetNone:
		return nil
	case model.MetricErasureTargetNode:
		return queryMetricErasureTarget(ctx, tx, "SELECT name FROM nodes WHERE id = ? AND lifecycle_state = 'active'"+
			" AND "+hiddenNodeFilter,
			request.TargetID, &request.TargetName)
	default:
		return queryMetricErasureTarget(ctx, tx,
			"SELECT name FROM network_tasks WHERE id = ? AND lifecycle_state = 'active'",
			request.TargetID, &request.TargetName)
	}
}

func queryMetricErasureTarget(ctx context.Context, tx *sql.Tx, query, id string, name *string) error {
	err := tx.QueryRowContext(ctx, query, id).Scan(name)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrMetricErasureMissing
	}
	if err != nil {
		return fmt.Errorf("resolve metric erasure target: %w", err)
	}
	return nil
}

// guardMetricErasureConcurrency 拒绝同一实体的第二个未完成任务，以及并发的舰队级擦除。
func guardMetricErasureConcurrency(
	ctx context.Context,
	tx *sql.Tx,
	request model.MetricErasureCreateRequest,
) error {
	query := `SELECT COUNT(*) FROM metric_erasure_jobs WHERE ` +
		`state IN ('queued','claimed','deleting','compacting','finalizing') AND `
	var count int
	var err error
	if request.TargetID == "" {
		err = tx.QueryRowContext(ctx, query+"scope = ?", request.Scope).Scan(&count)
	} else {
		err = tx.QueryRowContext(ctx, query+"target_type = ? AND target_id = ?",
			model.MetricErasureTargetTypeFor(request.Scope), request.TargetID).Scan(&count)
	}
	if err != nil {
		return fmt.Errorf("inspect active metric erasure jobs: %w", err)
	}
	if count > 0 {
		return model.ErrMetricErasureConflict
	}
	return nil
}

// tombstoneMetricErasureEntity 让将被删除的实体立刻不可见、不可用。
func tombstoneMetricErasureEntity(
	ctx context.Context,
	tx *sql.Tx,
	request model.MetricErasureCreateRequest,
	now time.Time,
) error {
	var query string
	switch request.Scope {
	case model.MetricErasureDeleteNode:
		if err := guardOperationNodeDelete(ctx, tx, request.TargetID); err != nil {
			return err
		}
		query = "UPDATE nodes SET lifecycle_state = 'deleting', updated_at = ? WHERE id = ? AND lifecycle_state = 'active'"
	case model.MetricErasureDeleteNetworkTask:
		query = "UPDATE network_tasks SET lifecycle_state = 'deleting', updated_at = ? WHERE id = ? AND lifecycle_state = 'active'"
	default:
		return nil
	}
	result, err := tx.ExecContext(ctx, query, now, request.TargetID)
	if err != nil {
		return fmt.Errorf("tombstone metric erasure target: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read metric erasure tombstone count: %w", err)
	}
	if affected != 1 {
		return model.ErrMetricErasureConflict
	}
	return nil
}

func insertMetricErasureJob(ctx context.Context, tx *sql.Tx, job model.MetricErasureJob) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO metric_erasure_jobs (`+metricErasureColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, metricErasureInsertArgs(job)...)
	if err != nil {
		if isUniqueConstraint(err) {
			return model.ErrMetricErasureConflict
		}
		return fmt.Errorf("append metric erasure job: %w", err)
	}
	if err := sessionRowsAffected(result, err, "append metric erasure job"); err != nil {
		return err
	}
	return nil
}

func metricErasureInsertArgs(job model.MetricErasureJob) []any {
	return []any{job.ID, job.Scope, job.TargetType, job.TargetID, job.TargetName, job.State,
		job.CutoffUnixNano, job.CatalogDigest, job.MeasurementCursor, job.AttemptCount, job.LeaseOwner,
		job.LeaseExpiresAt, job.NextAttemptAt, job.RequestID, job.ActorID, job.ActorName,
		job.SessionPrefix, job.LastErrorCode, job.RequestedAt, job.StartedAt, job.CompletedAt}
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}

// metricErasureEvidence 只登记范围与目标形状，绝不保存确认短语或实体地址。
func metricErasureEvidence(job model.MetricErasureJob) metadataEvidence {
	return metadataEvidence{Action: "metric_erasure." + job.Scope, Type: "metric_erasure",
		ID: job.ID, Name: job.TargetName, Fields: []string{"scope", "target_type", "request_id"}}
}
