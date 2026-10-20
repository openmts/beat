package store

import (
	"context"
	"fmt"
)

// MigrateApplicationDataLifecycle 引入应用历史保留设置与告警事件显示快照。
//
// 这里只保存策略与显示名快照：指标数值、探针样本、诊断与流量时间序列仍然只存在于 MTS。
// 备份生命周期扩展与恢复代次由 MigrateBackupLifecycle 在同一批次内继续落地。
func MigrateApplicationDataLifecycle(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS application_retention_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			alert_event_days INTEGER NOT NULL DEFAULT 180 CHECK(alert_event_days BETWEEN 1 AND 3650),
			audit_event_days INTEGER NOT NULL DEFAULT 180 CHECK(audit_event_days BETWEEN 1 AND 3650),
			cleanup_hour_utc INTEGER NOT NULL DEFAULT 3 CHECK(cleanup_hour_utc BETWEEN 0 AND 23),
			auto_cleanup_enabled INTEGER NOT NULL DEFAULT 1 CHECK(auto_cleanup_enabled IN (0, 1)),
			running INTEGER NOT NULL DEFAULT 0 CHECK(running IN (0, 1)),
			last_started_at DATETIME,
			last_completed_at DATETIME,
			last_status TEXT NOT NULL DEFAULT 'never' CHECK(last_status IN ('never', 'running', 'success', 'failed')),
			last_error_code TEXT NOT NULL DEFAULT '' CHECK(length(last_error_code) <= 64),
			last_trigger TEXT NOT NULL DEFAULT '' CHECK(length(last_trigger) <= 32),
			last_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(last_duration_ms >= 0),
			deleted_alert_events INTEGER NOT NULL DEFAULT 0 CHECK(deleted_alert_events >= 0),
			deleted_audit_events INTEGER NOT NULL DEFAULT 0 CHECK(deleted_audit_events >= 0),
			deleted_sessions INTEGER NOT NULL DEFAULT 0 CHECK(deleted_sessions >= 0),
			updated_at DATETIME NOT NULL
		)`,
		`INSERT OR IGNORE INTO application_retention_settings (
			id, alert_event_days, audit_event_days, cleanup_hour_utc, auto_cleanup_enabled,
			last_status, updated_at
		) VALUES (1, 180, 180, 3, 1, 'never', CURRENT_TIMESTAMP)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_triggered
			ON alert_events(triggered_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_status
			ON alert_events(status, resolved_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_rule
			ON alert_events(rule_id, triggered_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_node
			ON alert_events(node_id, triggered_at DESC, id DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply application data lifecycle schema: %w", err)
		}
	}
	columns := []struct {
		name string
		ddl  string
	}{
		{name: "rule_name_snapshot",
			ddl: "ALTER TABLE alert_events ADD COLUMN rule_name_snapshot TEXT NOT NULL DEFAULT ''"},
		{name: "node_name_snapshot",
			ddl: "ALTER TABLE alert_events ADD COLUMN node_name_snapshot TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		if err := addColumnIfMissing(ctx, db, "alert_events", column.name, column.ddl); err != nil {
			return err
		}
	}
	// 回填显示名快照：规则或节点已删除时保留空串，界面按“已删除”渲染，绝不回退到原始 ID。
	backfills := []string{
		`UPDATE alert_events SET rule_name_snapshot = COALESCE(
			(SELECT name FROM alert_rules WHERE alert_rules.id = alert_events.rule_id), '')
			WHERE rule_name_snapshot = ''`,
		`UPDATE alert_events SET node_name_snapshot = COALESCE(
			(SELECT name FROM nodes WHERE nodes.id = alert_events.node_id), '')
			WHERE node_name_snapshot = ''`,
	}
	for _, statement := range backfills {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill alert event snapshots: %w", err)
		}
	}
	// 同一批次继续落地备份生命周期与恢复代次列（见 MigrateBackupLifecycle）。
	return MigrateBackupLifecycle(ctx, db)
}

// addColumnIfMissing 让列扩展在重复执行与旧快照升级时可安全跳过。
func addColumnIfMissing(ctx context.Context, db schemaExecutor, table, column, ddl string) error {
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('"+table+"') WHERE name = ?", column).Scan(&count); err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if count != 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
