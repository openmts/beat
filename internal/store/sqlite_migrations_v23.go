package store

import (
	"context"
	"fmt"
)

// MigrateFleetSummary 引入舰队状态摘要的调度、作用域与运行审计表。
//
// 这里只保存管理员声明的调度元数据与有界运行审计：节点可用性来自已部署的可用性
// 状态机，通知正文与重试由通知 outbox 负责，指标数值仍只存在于 MTS。
// weekly 的 weekday 与前端表单一致使用 0–6（0 = 周日）。
func MigrateFleetSummary(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS fleet_summary_schedules (
			id TEXT PRIMARY KEY CHECK(length(id) = 36),
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
			cadence TEXT NOT NULL CHECK(cadence IN ('daily', 'weekly')),
			timezone TEXT NOT NULL CHECK(length(timezone) BETWEEN 1 AND 100),
			local_time TEXT NOT NULL CHECK(length(local_time) = 5),
			weekday INTEGER CHECK(weekday IS NULL OR weekday BETWEEN 0 AND 6),
			name_threshold INTEGER NOT NULL DEFAULT 10 CHECK(name_threshold BETWEEN 0 AND 50),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			next_run_at DATETIME NOT NULL,
			last_period_key TEXT NOT NULL DEFAULT '' CHECK(length(last_period_key) <= 128),
			last_run_at DATETIME,
			last_result TEXT NOT NULL DEFAULT '' CHECK(last_result IN ('', 'queued', 'success', 'partial', 'failed', 'unknown')),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			CHECK((cadence = 'weekly' AND weekday IS NOT NULL) OR (cadence = 'daily' AND weekday IS NULL))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fleet_summary_schedules_due
			ON fleet_summary_schedules(enabled, next_run_at)`,
		`CREATE TABLE IF NOT EXISTS fleet_summary_nodes (
			schedule_id TEXT NOT NULL REFERENCES fleet_summary_schedules(id) ON DELETE CASCADE,
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			PRIMARY KEY(schedule_id, node_id)
		)`,
		`CREATE TABLE IF NOT EXISTS fleet_summary_groups (
			schedule_id TEXT NOT NULL REFERENCES fleet_summary_schedules(id) ON DELETE CASCADE,
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			PRIMARY KEY(schedule_id, group_id)
		)`,
		`CREATE TABLE IF NOT EXISTS fleet_summary_channels (
			schedule_id TEXT NOT NULL REFERENCES fleet_summary_schedules(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
			PRIMARY KEY(schedule_id, channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS fleet_summary_runs (
			id TEXT PRIMARY KEY CHECK(length(id) = 36),
			schedule_id TEXT REFERENCES fleet_summary_schedules(id) ON DELETE SET NULL,
			schedule_name TEXT NOT NULL CHECK(length(schedule_name) BETWEEN 1 AND 100),
			cadence TEXT NOT NULL CHECK(cadence IN ('daily', 'weekly')),
			timezone TEXT NOT NULL CHECK(length(timezone) BETWEEN 1 AND 100),
			period_key TEXT NOT NULL CHECK(length(period_key) BETWEEN 1 AND 128),
			cutoff DATETIME NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('scheduled', 'test')),
			status TEXT NOT NULL CHECK(status IN ('queued', 'success', 'partial', 'failed')),
			online_count INTEGER NOT NULL DEFAULT 0 CHECK(online_count >= 0),
			offline_count INTEGER NOT NULL DEFAULT 0 CHECK(offline_count >= 0),
			total_count INTEGER NOT NULL DEFAULT 0 CHECK(total_count >= 0),
			online_names TEXT NOT NULL DEFAULT '[]',
			offline_names TEXT NOT NULL DEFAULT '[]',
			channel_count INTEGER NOT NULL DEFAULT 0 CHECK(channel_count >= 0),
			result_code TEXT NOT NULL DEFAULT '' CHECK(length(result_code) <= 64),
			created_at DATETIME NOT NULL,
			completed_at DATETIME
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS fleet_summary_run_period
			ON fleet_summary_runs(schedule_id, period_key, kind)`,
		`CREATE INDEX IF NOT EXISTS idx_fleet_summary_runs_schedule
			ON fleet_summary_runs(schedule_id, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply fleet status summary schema: %w", err)
		}
	}
	return nil
}
