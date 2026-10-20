package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// AgentRetentionStore 持久化发布对象保留期的后台设置与最近一轮清理结果。
type AgentRetentionStore struct {
	db *sql.DB
}

func NewAgentRetentionStore(db *sql.DB) *AgentRetentionStore {
	return &AgentRetentionStore{db: db}
}

const agentRetentionQuery = `SELECT configured, rollout_days, release_days, updated_at,
	last_run_at, last_status, last_error, last_rollouts, last_releases, last_artifacts,
	last_trigger FROM agent_release_retention WHERE id = 1`

// Get 读取单行设置；Configured=false 表示仍在使用启动环境变量缺省。
func (store *AgentRetentionStore) Get(ctx context.Context) (model.AgentRetentionSettings, error) {
	var settings model.AgentRetentionSettings
	var configured int
	var lastRun sql.NullTime
	err := store.db.QueryRowContext(ctx, agentRetentionQuery).Scan(&configured, &settings.RolloutDays,
		&settings.ReleaseDays, &settings.UpdatedAt, &lastRun, &settings.Status.LastStatus,
		&settings.Status.LastError, &settings.Status.LastRollouts, &settings.Status.LastReleases,
		&settings.Status.LastArtifacts, &settings.Status.LastTrigger)
	if err != nil {
		return settings, fmt.Errorf("get agent release retention settings: %w", err)
	}
	settings.Configured = configured == 1
	if lastRun.Valid {
		settings.Status.LastRunAt = &lastRun.Time
	}
	return settings, nil
}

// Update 写入管理员配置的保留期，并把 Configured 置为 1。
func (store *AgentRetentionStore) Update(ctx context.Context, settings model.AgentRetentionSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_release_retention SET configured = 1,
		rollout_days = ?, release_days = ?, updated_at = ? WHERE id = 1`,
		settings.RolloutDays, settings.ReleaseDays, model.NowUTC()); err != nil {
		return fmt.Errorf("update agent release retention settings: %w", err)
	}
	return nil
}

// Reset 回到启动环境变量缺省；只清配置，保留最近一轮清理记录。
func (store *AgentRetentionStore) Reset(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_release_retention SET configured = 0,
		rollout_days = 0, release_days = 0, updated_at = ? WHERE id = 1`, model.NowUTC()); err != nil {
		return fmt.Errorf("reset agent release retention settings: %w", err)
	}
	return nil
}

// MarkRun 记录一轮清理的结果；失败时保留可读原因供后台展示。
func (store *AgentRetentionStore) MarkRun(
	ctx context.Context,
	status string,
	reason string,
	trigger string,
	counts model.AgentRetentionRunStatus,
) error {
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_release_retention SET last_run_at = ?,
		last_status = ?, last_error = ?, last_rollouts = ?, last_releases = ?, last_artifacts = ?,
		last_trigger = ? WHERE id = 1`, model.NowUTC(), status, reason, counts.LastRollouts,
		counts.LastReleases, counts.LastArtifacts, trigger); err != nil {
		return fmt.Errorf("record agent release retention run: %w", err)
	}
	return nil
}
