package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/beat/backend/internal/model"
)

// Policy 返回服务端诊断策略；没有记录时返回关闭状态的默认策略。
func (service *Service) Policy(ctx context.Context, actor Actor) (model.DiagnosticPolicy, error) {
	var policy model.DiagnosticPolicy
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		loaded, err := tx.policy()
		if err != nil {
			return err
		}
		policy = loaded
		return nil
	})
	return policy, err
}

// SavePolicy 只允许 owner 在最近重认证后扩展或收紧诊断策略。
func (service *Service) SavePolicy(ctx context.Context, actor Actor, policy model.DiagnosticPolicy) (model.DiagnosticPolicy, error) {
	if !service.enabled {
		return model.DiagnosticPolicy{}, ErrForbidden
	}
	if err := policy.Validate(); err != nil {
		return model.DiagnosticPolicy{}, ErrInvalid
	}
	var saved model.DiagnosticPolicy
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		current, err := tx.policy()
		if err != nil {
			return err
		}
		if policy.Revision != current.Revision {
			return ErrConflict
		}
		policy.Revision = current.Revision + 1
		policy.UpdatedBy = actor.UserID
		policy.UpdatedAt = tx.now
		if err := tx.savePolicy(policy); err != nil {
			return err
		}
		if err := tx.audit(actor.UserID, "diagnostic.policy.updated", policy.ID); err != nil {
			return err
		}
		saved = policy
		return nil
	})
	return saved, err
}

func (tx transaction) policy() (model.DiagnosticPolicy, error) {
	policy := model.DefaultDiagnosticPolicy()
	var kinds string
	var updatedAt sql.NullTime
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT enabled, allowed_kinds_json, allow_udp, allow_reverse,
		allow_private_targets, maximum_active_jobs, maximum_active_jobs_per_node, daily_byte_budget,
		maximum_iperf_duration_seconds, maximum_iperf_bitrate_mbps, revision, updated_by, updated_at
		FROM diagnostic_policies WHERE id='default'`).Scan(&policy.Enabled, &kinds, &policy.AllowUDP, &policy.AllowReverse,
		&policy.AllowPrivateTargets, &policy.MaximumActiveJobs, &policy.MaximumActiveJobsPerNode,
		&policy.DailyByteBudget, &policy.MaximumIperfDuration, &policy.MaximumIperfBitrate,
		&policy.Revision, &policy.UpdatedBy, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DefaultDiagnosticPolicy(), nil
	}
	if err != nil {
		return model.DiagnosticPolicy{}, ErrUnavailable
	}
	if err := json.Unmarshal([]byte(kinds), &policy.AllowedKinds); err != nil {
		return model.DiagnosticPolicy{}, ErrUnavailable
	}
	if updatedAt.Valid {
		policy.UpdatedAt = updatedAt.Time
	}
	if err := policy.Validate(); err != nil {
		return model.DiagnosticPolicy{}, ErrUnavailable
	}
	return policy, nil
}

func (tx transaction) savePolicy(policy model.DiagnosticPolicy) error {
	encoded, err := json.Marshal(policy.AllowedKinds)
	if err != nil {
		return ErrInvalid
	}
	return tx.one(`INSERT INTO diagnostic_policies(id, enabled, allowed_kinds_json, allow_udp, allow_reverse,
		allow_private_targets, maximum_active_jobs, maximum_active_jobs_per_node, daily_byte_budget,
		maximum_iperf_duration_seconds, maximum_iperf_bitrate_mbps, revision, updated_by, updated_at)
	 VALUES('default',?,?,?,?,?,?,?,?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled, allowed_kinds_json=excluded.allowed_kinds_json,
		allow_udp=excluded.allow_udp, allow_reverse=excluded.allow_reverse,
		allow_private_targets=excluded.allow_private_targets, maximum_active_jobs=excluded.maximum_active_jobs,
		maximum_active_jobs_per_node=excluded.maximum_active_jobs_per_node, daily_byte_budget=excluded.daily_byte_budget,
		maximum_iperf_duration_seconds=excluded.maximum_iperf_duration_seconds,
		maximum_iperf_bitrate_mbps=excluded.maximum_iperf_bitrate_mbps, revision=excluded.revision,
		updated_by=excluded.updated_by, updated_at=excluded.updated_at`,
		policy.Enabled, string(encoded), policy.AllowUDP, policy.AllowReverse, policy.AllowPrivateTargets,
		policy.MaximumActiveJobs, policy.MaximumActiveJobsPerNode, policy.DailyByteBudget,
		policy.MaximumIperfDuration, policy.MaximumIperfBitrate, policy.Revision, policy.UpdatedBy, tx.now)
}

// allowsKind 报告策略是否允许某类作业，并给出有效的 iperf3 约束。
func allowsKind(policy model.DiagnosticPolicy, kind string) bool {
	return slices.Contains(policy.AllowedKinds, kind)
}

func effectiveDuration(policy model.DiagnosticPolicy, requested int) int {
	return min(requested, policy.MaximumIperfDuration)
}

func effectiveBitrate(policy model.DiagnosticPolicy, requested int) int {
	return min(requested, policy.MaximumIperfBitrate)
}
