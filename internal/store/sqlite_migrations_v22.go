package store

import (
	"context"
	"fmt"
)

// MigrateMetricErasure 引入耐久擦除任务与实体生命周期标记。
//
// SQLite 只保存任务、策略、实体快照与审计：指标样本永远只存在于 MTS。节点与网络
// 任务的 lifecycle_state 让“已请求删除但尚未擦除”的实体立刻从常规列表中消失，
// 同时保留可重试的耐久任务。
func MigrateMetricErasure(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS metric_erasure_jobs (
			id TEXT PRIMARY KEY CHECK(length(id) = 36),
			scope TEXT NOT NULL CHECK(scope IN ('node_history','network_task_history',
				'fleet_telemetry','all_metric_history','delete_node','delete_network_task')),
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '' CHECK(length(target_id) <= 64),
			target_name TEXT NOT NULL DEFAULT '' CHECK(length(target_name) <= 128),
			state TEXT NOT NULL CHECK(state IN ('queued','claimed','deleting','compacting',
				'finalizing','succeeded','failed','canceled')),
			cutoff_unix_nano INTEGER,
			catalog_digest TEXT NOT NULL DEFAULT '' CHECK(length(catalog_digest) <= 64),
			measurement_cursor TEXT NOT NULL DEFAULT '' CHECK(length(measurement_cursor) <= 128),
			attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0),
			lease_owner TEXT NOT NULL DEFAULT '' CHECK(length(lease_owner) <= 64),
			lease_expires_at DATETIME,
			next_attempt_at DATETIME,
			request_id TEXT NOT NULL CHECK(length(request_id) = 36),
			actor_id TEXT NOT NULL DEFAULT '' CHECK(length(actor_id) <= 64),
			actor_name TEXT NOT NULL DEFAULT '' CHECK(length(actor_name) <= 64),
			session_prefix TEXT NOT NULL DEFAULT '' CHECK(length(session_prefix) <= 32),
			last_error_code TEXT NOT NULL DEFAULT '' CHECK(length(last_error_code) <= 64),
			requested_at DATETIME NOT NULL,
			started_at DATETIME,
			completed_at DATETIME,
			CHECK (
				(scope IN ('node_history','delete_node') AND target_type = 'node' AND target_id <> '')
				OR (scope IN ('network_task_history','delete_network_task')
					AND target_type = 'network_task' AND target_id <> '')
				OR (scope IN ('fleet_telemetry','all_metric_history') AND target_type = '' AND target_id = ''))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS metric_erasure_request
			ON metric_erasure_jobs(request_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS metric_erasure_active_target
			ON metric_erasure_jobs(target_type, target_id) WHERE target_id <> ''
			AND state IN ('queued','claimed','deleting','compacting','finalizing')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS metric_erasure_active_fleet
			ON metric_erasure_jobs(scope) WHERE scope IN ('fleet_telemetry','all_metric_history')
			AND state IN ('queued','claimed','deleting','compacting','finalizing')`,
		`CREATE INDEX IF NOT EXISTS metric_erasure_recent ON metric_erasure_jobs(requested_at DESC, id DESC)`,
		`ALTER TABLE nodes ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE network_tasks ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active'`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_lifecycle ON nodes(lifecycle_state)`,
		`CREATE INDEX IF NOT EXISTS idx_network_tasks_lifecycle ON network_tasks(lifecycle_state)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply metric erasure schema: %w", err)
		}
	}
	return nil
}
