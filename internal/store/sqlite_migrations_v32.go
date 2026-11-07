package store

import (
	"context"
	"fmt"
)

// applyNetworkQualityDirections 增加网络质量探测方向与 ICMP 包数。
// 已有任务统一保持 agent_to_target 和单包行为；新任务默认由应用层使用 5 包 ICMP。
func applyNetworkQualityDirections(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`ALTER TABLE network_tasks ADD COLUMN direction TEXT NOT NULL DEFAULT 'agent_to_target'
			CHECK(direction IN ('agent_to_target', 'server_to_agent'))`,
		`ALTER TABLE network_tasks ADD COLUMN packet_count INTEGER NOT NULL DEFAULT 5
			CHECK(packet_count BETWEEN 1 AND 20)`,
		`UPDATE network_tasks SET packet_count = 1`,
		`CREATE INDEX IF NOT EXISTS idx_network_tasks_direction
			ON network_tasks(direction, enabled, lifecycle_state)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply network quality direction schema: %w", err)
		}
	}
	return nil
}
