package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// ErrPublicTelemetryConflict 表示提交的版本号与已持久化的策略不一致。
var ErrPublicTelemetryConflict = errors.New("public telemetry policy revision conflict")

// PublicTelemetryStore 只保存匿名聚合遥测策略；样本永远只进 MTS。
type PublicTelemetryStore struct {
	db *sql.DB
}

func NewPublicTelemetryStore(db *sql.DB) *PublicTelemetryStore {
	return &PublicTelemetryStore{db: db}
}

const publicTelemetrySelect = `SELECT enabled,allowed_events_json,sample_ceiling,retention_days,revision,updated_by,updated_at FROM public_telemetry_policy WHERE id=1`

func (store *PublicTelemetryStore) Get(ctx context.Context) (model.PublicTelemetryPolicy, error) {
	return scanPublicTelemetryPolicy(store.db.QueryRowContext(ctx, publicTelemetrySelect))
}

func scanPublicTelemetryPolicy(row rowScanner) (model.PublicTelemetryPolicy, error) {
	var policy model.PublicTelemetryPolicy
	var enabled int
	var allowed string
	err := row.Scan(&enabled, &allowed, &policy.SampleCeiling, &policy.RetentionDays, &policy.Revision,
		&policy.UpdatedBy, &policy.UpdatedAt)
	if err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	policy.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(allowed), &policy.AllowedEvents); err != nil {
		return model.PublicTelemetryPolicy{}, fmt.Errorf("decode public telemetry events: %w", err)
	}
	return policy, nil
}

// Update 原子地保存策略、删除前值审计并把保留期写回两个计数测量。
func (store *PublicTelemetryStore) Update(
	ctx context.Context,
	policy model.PublicTelemetryPolicy,
) (model.PublicTelemetryPolicy, error) {
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	before, err := scanPublicTelemetryPolicy(tx.QueryRowContext(ctx, publicTelemetrySelect))
	if err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	if policy.Revision != before.Revision+1 {
		return model.PublicTelemetryPolicy{}, ErrPublicTelemetryConflict
	}
	policy.Normalize()
	if err := policy.Validate(); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	policy.UpdatedAt = model.NowUTC()
	if err := writePublicTelemetryPolicy(ctx, tx, policy); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	if err := applyPublicTelemetryRetention(ctx, tx, policy); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	if err := mutation.finish(publicTelemetryEvidence(before, policy)); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	actual, err := scanPublicTelemetryPolicy(tx.QueryRowContext(ctx, publicTelemetrySelect))
	if err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	if !publicTelemetryPolicyEqual(actual, policy) {
		return model.PublicTelemetryPolicy{}, errors.New("public telemetry policy was not persisted")
	}
	if err := tx.Commit(); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	return policy, nil
}

func writePublicTelemetryPolicy(ctx context.Context, tx *sql.Tx, policy model.PublicTelemetryPolicy) error {
	// []string 的 JSON 编码不会失败；这里只需要一个有界的结果。
	allowed, _ := json.Marshal(policy.AllowedEvents)
	if len(allowed) > 512 {
		return errors.New("public telemetry events exceed their bound")
	}
	result, err := tx.ExecContext(ctx, `UPDATE public_telemetry_policy SET enabled = ?, allowed_events_json = ?,
		sample_ceiling = ?, retention_days = ?, revision = ?, updated_by = ?, updated_at = ? WHERE id = 1`,
		policy.Enabled, string(allowed), policy.SampleCeiling, policy.RetentionDays, policy.Revision, policy.UpdatedBy, policy.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update public telemetry policy: %w", err)
	}
	return sessionRowsAffected(result, err, "update public telemetry policy")
}

// applyPublicTelemetryRetention 把保留期写回计数测量的覆盖值；目录尚未播种时是空操作。
func applyPublicTelemetryRetention(ctx context.Context, tx *sql.Tx, policy model.PublicTelemetryPolicy) error {
	for _, key := range model.PublicInteractionMetrics() {
		if _, err := tx.ExecContext(ctx,
			`UPDATE metric_definitions SET retention_days = ?, updated_at = ? WHERE metric_key = ?`,
			policy.RetentionDays, policy.UpdatedAt, key); err != nil {
			return fmt.Errorf("apply public telemetry retention to %s: %w", key, err)
		}
	}
	return nil
}
