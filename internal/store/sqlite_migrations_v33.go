package store

import (
	"context"
	"fmt"
)

// applyNetworkQualityTargets 把 Agent 侧网络任务扩展为可配置的多目标模型。
// 服务端探测任务仍使用 network_tasks.target 模板；已有 Agent 任务迁移为单目标记录。
func applyNetworkQualityTargets(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`ALTER TABLE network_tasks ADD COLUMN target_mode TEXT NOT NULL DEFAULT 'single'
			CHECK(target_mode IN ('single', 'group'))`,
		`CREATE TABLE IF NOT EXISTS network_task_targets (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 100),
			carrier TEXT NOT NULL CHECK(carrier IN ('mobile', 'unicom', 'telecom', 'custom')),
			region TEXT NOT NULL DEFAULT '' CHECK(length(region) <= 100),
			target TEXT NOT NULL CHECK(length(CAST(target AS BLOB)) BETWEEN 1 AND 2048),
			ip_family TEXT NOT NULL CHECK(ip_family IN ('auto', 'ipv4', 'ipv6')),
			packet_count INTEGER NOT NULL CHECK(packet_count BETWEEN 1 AND 20),
			visible_address INTEGER NOT NULL DEFAULT 0 CHECK(visible_address IN (0, 1)),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			sort_order INTEGER NOT NULL DEFAULT 0 CHECK(sort_order >= 0),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(task_id) REFERENCES network_tasks(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_network_task_targets_task
			ON network_task_targets(task_id, sort_order, id)`,
		`INSERT INTO network_task_targets (
			id, task_id, label, carrier, region, target, ip_family, packet_count,
			visible_address, enabled, sort_order, created_at, updated_at
		)
		SELECT lower(hex(randomblob(16))), id, name, 'custom', '', target, ip_family,
			packet_count, 0, 1, 0, created_at, updated_at
		FROM network_tasks
		WHERE direction = 'agent_to_target'
			AND NOT EXISTS (SELECT 1 FROM network_task_targets t WHERE t.task_id = network_tasks.id)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply network quality target schema: %w", err)
		}
	}
	return nil
}
