package store

import (
	"context"
	"fmt"
)

// MigrateTrafficCalibration 引入追加式的流量校准账本。
//
// 这里只保存管理员声明的账目目标与操作者快照：原始 MTS 流量点、派生增量和任何
// 数值时间序列都不进入 SQLite。事件只追加，纠正通过“一条 revoke + 一条新 set”表达。
// node_id 在节点删除后置空，事件与其节点名快照仍然保留，但不再参与结算解析。
func MigrateTrafficCalibration(ctx context.Context, db schemaExecutor) error {
	constraints := `
		CHECK (
			(kind = 'set' AND effective_at IS NOT NULL AND target_sent IS NOT NULL
				AND target_received IS NOT NULL AND reference_event_id IS NULL
				AND target_sent BETWEEN 0 AND 9007199254740992
				AND target_received BETWEEN 0 AND 9007199254740992)
			OR
			(kind = 'revoke' AND effective_at IS NULL AND target_sent IS NULL
				AND target_received IS NULL AND reference_event_id IS NOT NULL)
		)`
	statements := []string{
		`CREATE TABLE IF NOT EXISTS traffic_calibration_events (
			id TEXT PRIMARY KEY CHECK(length(id) = 36),
			node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
			node_name TEXT NOT NULL CHECK(length(node_name) BETWEEN 1 AND 128),
			kind TEXT NOT NULL CHECK(kind IN ('set', 'revoke')),
			effective_at DATETIME,
			target_sent INTEGER,
			target_received INTEGER,
			reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 500),
			actor_id TEXT NOT NULL CHECK(length(actor_id) BETWEEN 1 AND 64),
			actor_name TEXT NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 64),
			session_id TEXT NOT NULL DEFAULT '' CHECK(length(session_id) <= 64),
			request_id TEXT NOT NULL CHECK(length(request_id) = 36),
			reference_event_id TEXT REFERENCES traffic_calibration_events(id),
			created_at DATETIME NOT NULL` + constraints + `,
			CHECK(reference_event_id IS NULL OR reference_event_id <> id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS traffic_calibration_request
			ON traffic_calibration_events(request_id)`,
		`CREATE INDEX IF NOT EXISTS traffic_calibration_node
			ON traffic_calibration_events(node_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS traffic_calibration_reference
			ON traffic_calibration_events(reference_event_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply traffic calibration schema: %w", err)
		}
	}
	return nil
}
