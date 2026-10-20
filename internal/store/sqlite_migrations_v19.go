package store

import (
	"context"
	"fmt"
)

// MigrateAdvancedDiagnostics 引入受控路径/带宽诊断的目标、策略、作业、尝试、
// 跳转身份与每日字节预算账本。
//
// 数值结果只写 MTS；这里只保存应用状态与有界非数值元数据。v19 之前的保留条目
// 从未随任何发布分发。
func MigrateAdvancedDiagnostics(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		// 诊断跳身份把密钥生命周期源数量提升到 15，游标终值必须能被约束容纳。
		`ALTER TABLE application_secret_state RENAME TO application_secret_state_v18`,
		`CREATE TABLE application_secret_state (
 id INTEGER PRIMARY KEY CHECK(id=1),phase TEXT NOT NULL CHECK(phase IN('pending','backfill','sanitize','ready','rotating')),
 generation INTEGER NOT NULL CHECK(generation>=0),root_generation INTEGER NOT NULL CHECK(root_generation>=0),
 root_fingerprint TEXT NOT NULL,active_key_id TEXT NOT NULL,cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 15),
 cursor_id TEXT NOT NULL,rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id='' OR length(rotation_id)=36),updated_at INTEGER NOT NULL)`,
		`INSERT INTO application_secret_state SELECT * FROM application_secret_state_v18`,
		`DROP TABLE application_secret_state_v18`,
		`CREATE TABLE IF NOT EXISTS diagnostic_policies (
			id TEXT PRIMARY KEY CHECK(id = 'default'),
			enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0, 1)),
			allowed_kinds_json TEXT NOT NULL DEFAULT '["mtr"]',
			allow_udp INTEGER NOT NULL DEFAULT 0 CHECK(allow_udp IN (0, 1)),
			allow_reverse INTEGER NOT NULL DEFAULT 0 CHECK(allow_reverse IN (0, 1)),
			allow_private_targets INTEGER NOT NULL DEFAULT 0 CHECK(allow_private_targets IN (0, 1)),
			maximum_active_jobs INTEGER NOT NULL DEFAULT 2,
			maximum_active_jobs_per_node INTEGER NOT NULL DEFAULT 1,
			daily_byte_budget INTEGER NOT NULL DEFAULT 0 CHECK(daily_byte_budget >= 0),
			maximum_iperf_duration_seconds INTEGER NOT NULL DEFAULT 30,
			maximum_iperf_bitrate_mbps INTEGER NOT NULL DEFAULT 1000,
			revision INTEGER NOT NULL DEFAULT 1,
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_targets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			purpose TEXT NOT NULL DEFAULT '',
			kinds_json TEXT NOT NULL DEFAULT '["mtr"]',
			address TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 0 CHECK(port BETWEEN 0 AND 65535),
			ip_family TEXT NOT NULL CHECK(ip_family IN ('auto', 'ipv4', 'ipv6')),
			protocol TEXT NOT NULL CHECK(protocol IN ('icmp', 'tcp', 'udp')),
			direction TEXT NOT NULL CHECK(direction IN ('forward', 'reverse')),
			private_address INTEGER NOT NULL DEFAULT 0 CHECK(private_address IN (0, 1)),
			allowed_addresses_json TEXT NOT NULL DEFAULT '[]',
			resolved_addresses_json TEXT NOT NULL DEFAULT '[]',
			resolved_at DATETIME,
			maximum_bitrate_mbps INTEGER NOT NULL DEFAULT 1000,
			maximum_duration_seconds INTEGER NOT NULL DEFAULT 30,
			maximum_parallel_streams INTEGER NOT NULL DEFAULT 4,
			daily_byte_budget INTEGER NOT NULL DEFAULT 0 CHECK(daily_byte_budget >= 0),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			expires_at DATETIME,
			revision INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL,
			updated_by TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnostic_targets_enabled
			ON diagnostic_targets(enabled, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_jobs (
			id TEXT PRIMARY KEY,
			source_node_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('mtr', 'iperf3')),
			state TEXT NOT NULL CHECK(state IN ('queued', 'leased', 'running', 'succeeded',
				'partial', 'failed', 'timed_out', 'cancelled', 'interrupted', 'unknown')),
			actor_user_id TEXT NOT NULL,
			actor_username TEXT NOT NULL DEFAULT '',
			confirmation TEXT NOT NULL DEFAULT '',
			source_node_name TEXT NOT NULL DEFAULT '',
			target_name TEXT NOT NULL DEFAULT '',
			target_revision INTEGER NOT NULL DEFAULT 1,
			policy_revision INTEGER NOT NULL DEFAULT 1,
			protocol TEXT NOT NULL DEFAULT 'icmp',
			direction TEXT NOT NULL DEFAULT 'forward',
			ip_family TEXT NOT NULL DEFAULT 'auto',
			probes INTEGER NOT NULL DEFAULT 10,
			hop_limit INTEGER NOT NULL DEFAULT 32,
			duration_seconds INTEGER NOT NULL DEFAULT 10,
			parallel_streams INTEGER NOT NULL DEFAULT 1,
			bitrate_mbps INTEGER NOT NULL DEFAULT 100,
			policy_snapshot_json TEXT NOT NULL DEFAULT '{}',
			target_snapshot_json TEXT NOT NULL DEFAULT '{}',
			capability_digest TEXT NOT NULL DEFAULT '',
			reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK(reserved_bytes >= 0),
			actual_bytes INTEGER NOT NULL DEFAULT 0 CHECK(actual_bytes >= 0),
			attempt INTEGER NOT NULL DEFAULT 0,
			cursor_sequence INTEGER NOT NULL DEFAULT 0,
			cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0, 1)),
			failure_code TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			leased_at DATETIME,
			started_at DATETIME,
			completed_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnostic_jobs_state
			ON diagnostic_jobs(state, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnostic_jobs_node
			ON diagnostic_jobs(source_node_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_attempts (
			job_id TEXT NOT NULL,
			attempt INTEGER NOT NULL CHECK(attempt >= 1),
			state TEXT NOT NULL CHECK(state IN ('leased', 'running', 'completed', 'interrupted', 'unknown')),
			lease_nonce TEXT NOT NULL DEFAULT '',
			capability_digest TEXT NOT NULL DEFAULT '',
			failure_code TEXT NOT NULL DEFAULT '',
			leased_at DATETIME NOT NULL,
			started_at DATETIME,
			heartbeat_at DATETIME,
			completed_at DATETIME,
			PRIMARY KEY (job_id, attempt)
		)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_hop_identities (
			job_id TEXT NOT NULL,
			hop_index INTEGER NOT NULL CHECK(hop_index BETWEEN 1 AND 64),
			identity_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY (job_id, hop_index)
		)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_capabilities (
			node_id TEXT PRIMARY KEY,
			digest TEXT NOT NULL,
			document_json TEXT NOT NULL CHECK(length(document_json) <= 16384),
			seen_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_budget_ledger (
			day TEXT NOT NULL,
			node_id TEXT NOT NULL,
			reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK(reserved_bytes >= 0),
			actual_bytes INTEGER NOT NULL DEFAULT 0 CHECK(actual_bytes >= 0),
			PRIMARY KEY (day, node_id)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply advanced diagnostics schema: %w", err)
		}
	}
	return nil
}
