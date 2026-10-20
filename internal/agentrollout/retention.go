package agentrollout

import (
	"context"
	"errors"
	"time"
)

const (
	// retentionBatchSize 限制单个事务删除的对象数量，避免自动清理长时间占用写锁。
	retentionBatchSize = 16
	// maximumRetentionDays 上限十年，拦截把秒数或毫秒数写成天数的误配。
	maximumRetentionDays = 3650
	// systemUsername 是自动化清理写审计时使用的操作者名。
	systemUsername = "system"
)

// RetentionPolicy 描述发布对象的保留期；零值表示不清理，保持默认的保守行为。
// 只有显式配置保留期的实例才会启动自动清理。
type RetentionPolicy struct {
	RolloutDays int
	ReleaseDays int
}

// Enabled 报告策略是否配置了任一保留期。
func (policy RetentionPolicy) Enabled() bool {
	return policy.RolloutDays > 0 || policy.ReleaseDays > 0
}

// Valid 报告两个保留期是否都在允许区间内（0 表示保留全部历史）。
func (policy RetentionPolicy) Valid() bool {
	return withinRetentionBounds(policy.RolloutDays) && withinRetentionBounds(policy.ReleaseDays)
}

func withinRetentionBounds(days int) bool {
	return days >= 0 && days <= maximumRetentionDays
}

// RetentionCounts 汇总一次自动清理删除的对象数量。
type RetentionCounts struct {
	Rollouts  int
	Releases  int
	Artifacts int
}

// Prune 删除超过保留期的终态 rollout，以及超过保留期、已 revoke 且没有 rollout 引用的发布。
// 产物文件只在摘要不再被任何发布引用时删除。策略为零值时不做任何事，也不写审计。
func (s *Service) Prune(ctx context.Context, policy RetentionPolicy) (RetentionCounts, error) {
	if err := s.requireEnabled(); err != nil {
		return RetentionCounts{}, err
	}
	if !policy.Valid() {
		return RetentionCounts{}, ErrInvalid
	}
	if !policy.Enabled() {
		return RetentionCounts{}, nil
	}
	counts := RetentionCounts{}
	now := s.now()
	if policy.RolloutDays > 0 {
		deleted, err := s.pruneRollouts(ctx, now.AddDate(0, 0, -policy.RolloutDays))
		counts.Rollouts = deleted
		if err != nil {
			return counts, err
		}
	}
	if policy.ReleaseDays > 0 {
		releases, artifacts, err := s.pruneReleases(ctx, now.AddDate(0, 0, -policy.ReleaseDays))
		counts.Releases, counts.Artifacts = releases, artifacts
		if err != nil {
			return counts, err
		}
	}
	return counts, nil
}

// pruneRollouts 反复按批删除更新时刻早于 cutoff 的终态 rollout，直到没有更多可删行。
func (s *Service) pruneRollouts(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	for {
		ids, err := s.expiredRollouts(ctx, cutoff)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		deleted, err := s.deleteExpiredRollouts(ctx, ids)
		total += deleted
		if err != nil {
			return total, err
		}
		if deleted < len(ids) {
			// 有对象在选定之后重新进入在途状态：本轮到此为止，下一轮再评估。
			return total, nil
		}
	}
}

// deleteExpiredRollouts 在同一个系统事务里删除一批 rollout 并为每个对象写审计，返回实际删除数。
func (s *Service) deleteExpiredRollouts(ctx context.Context, ids []string) (int, error) {
	deleted := 0
	err := s.mutateSystem(ctx, func(tx *transaction) error {
		for _, id := range ids {
			targets, err := tx.deleteRollout(id)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			event := audit{Action: "rollout.delete", Kind: "rollout", ID: id, Reason: "retention", Count: targets}
			if err := tx.audit(event); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	if err != nil {
		return deleted, err
	}
	return deleted, nil
}

// pruneReleases 反复按批删除已 revoke、撤销时刻早于 cutoff 且没有 rollout 引用的发布。
func (s *Service) pruneReleases(ctx context.Context, cutoff time.Time) (int, int, error) {
	releases, artifacts := 0, 0
	orphans := []string{}
	for {
		ids, err := s.expiredReleases(ctx, cutoff)
		if err != nil {
			return releases, artifacts, err
		}
		if len(ids) == 0 {
			s.removeArtifacts(orphans)
			return releases, artifacts, nil
		}
		removed, count, deleted, err := s.deleteExpiredReleases(ctx, ids)
		if err != nil {
			return releases, artifacts, err
		}
		orphans = append(orphans, removed...)
		releases += deleted
		artifacts += count
		if deleted < len(ids) {
			// 有发布在选定之后重新被 rollout 引用：本轮到此为止，下一轮再评估。
			s.removeArtifacts(orphans)
			return releases, artifacts, nil
		}
	}
}

// deleteExpiredReleases 在同一个系统事务里删除一批发布，返回孤儿摘要、产物行数与实际删除数。
func (s *Service) deleteExpiredReleases(ctx context.Context, ids []string) ([]string, int, int, error) {
	orphans, artifacts, deleted := []string{}, 0, 0
	err := s.mutateSystem(ctx, func(tx *transaction) error {
		for _, id := range ids {
			deletion, err := tx.deleteRelease(id)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			event := audit{Action: "release.delete", Kind: "release", ID: id, Reason: "retention", Count: deletion.artifacts}
			if err := tx.audit(event); err != nil {
				return err
			}
			orphans = append(orphans, deletion.orphans...)
			artifacts += deletion.artifacts
			deleted++
		}
		return nil
	})
	if err != nil {
		return nil, 0, deleted, err
	}
	return orphans, artifacts, deleted, nil
}

func (s *Service) expiredRollouts(ctx context.Context, cutoff time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM agent_rollouts
WHERE state IN('completed','aborted') AND updated_at<? ORDER BY updated_at LIMIT ?`, cutoff, retentionBatchSize)
	if err != nil {
		return nil, ErrUnavailable
	}
	return scanIdentifierColumn(rows)
}

func (s *Service) expiredReleases(ctx context.Context, cutoff time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id FROM agent_releases r
WHERE r.revoked_at IS NOT NULL AND r.revoked_at<?
AND NOT EXISTS(SELECT 1 FROM agent_rollouts o WHERE o.release_id=r.id)
ORDER BY r.revoked_at LIMIT ?`, cutoff, retentionBatchSize)
	if err != nil {
		return nil, ErrUnavailable
	}
	return scanIdentifierColumn(rows)
}

// mutateSystem 以系统身份执行一次写入事务：没有管理员会话，审计记在 system 名下。
func (s *Service) mutateSystem(ctx context.Context, action func(*transaction) error) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	db, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: s.now(), username: systemUsername}
	if err := action(tx); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}
