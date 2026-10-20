package store

import "context"

// MigrateOperations 引入默认关闭的运维域；不会启用任何 Agent 能力。
func MigrateOperations(ctx context.Context, db schemaExecutor) error {
	for _, statement := range operationSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return executeSchemaStatements(ctx, db, operationLifecycleSchema)
}

var operationSchema = []string{
	`CREATE TABLE IF NOT EXISTS operation_policies (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
		configuration_json TEXT NOT NULL CHECK(length(configuration_json)<=16384),
		version INTEGER NOT NULL CHECK(version>0), creator_id TEXT NOT NULL,
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_policy_nodes (
		policy_id TEXT NOT NULL REFERENCES operation_policies(id) ON DELETE CASCADE,
		node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, PRIMARY KEY(policy_id,node_id))`,
	`CREATE TABLE IF NOT EXISTS operation_policy_groups (
		policy_id TEXT NOT NULL REFERENCES operation_policies(id) ON DELETE CASCADE,
		group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE, PRIMARY KEY(policy_id,group_id))`,
	`CREATE TABLE IF NOT EXISTS operation_snippets (
		id TEXT PRIMARY KEY, name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
		description TEXT NOT NULL DEFAULT '' CHECK(length(description)<=2000),
		visibility TEXT NOT NULL CHECK(visibility IN ('private','shared')), owner_id TEXT NOT NULL,
		current_version INTEGER NOT NULL CHECK(current_version>0), deleted_at INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_snippet_versions (
		id TEXT PRIMARY KEY, snippet_id TEXT NOT NULL REFERENCES operation_snippets(id), version INTEGER NOT NULL CHECK(version>0),
		shell_family TEXT NOT NULL CHECK(shell_family IN ('sh','powershell')), content_encrypted BLOB NOT NULL,
		fingerprint TEXT NOT NULL, approved_by TEXT, approved_at INTEGER, created_at INTEGER NOT NULL,
		UNIQUE(snippet_id,version))`,
	`CREATE TABLE IF NOT EXISTS operation_tasks (
		id TEXT PRIMARY KEY, dedupe_key TEXT NOT NULL UNIQUE, request_fingerprint TEXT NOT NULL,
		kind TEXT NOT NULL CHECK(kind IN ('snippet','shell','deploy','collect')), payload_encrypted BLOB NOT NULL,
		policy_id TEXT NOT NULL REFERENCES operation_policies(id), policy_snapshot TEXT NOT NULL, actor_id TEXT NOT NULL,
		timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds BETWEEN 1 AND 3600),
		output_bytes INTEGER NOT NULL CHECK(output_bytes BETWEEN 1 AND 1048576),
		expires_at INTEGER NOT NULL, cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0,1)),
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_task_targets (
		id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES operation_tasks(id),
		node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL, node_name TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('queued','leased','running','unknown','cancel_requested','succeeded','failed','timed_out','canceled','output_limit','interrupted','expired')),
		current_attempt TEXT NOT NULL DEFAULT '', retry_actor TEXT NOT NULL DEFAULT '', retry_reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		UNIQUE(task_id,node_id))`,
	`CREATE TABLE IF NOT EXISTS operation_attempts (
		id TEXT PRIMARY KEY, target_id TEXT NOT NULL REFERENCES operation_task_targets(id),
		attempt_number INTEGER NOT NULL CHECK(attempt_number>0), nonce_hash BLOB NOT NULL CHECK(length(nonce_hash)=32),
		state TEXT NOT NULL CHECK(state IN ('leased','running','unknown','cancel_requested','succeeded','failed','timed_out','canceled','output_limit','interrupted','expired')),
		capability_digest TEXT NOT NULL, lease_expires_at INTEGER NOT NULL, started_at INTEGER, heartbeat_at INTEGER,
		stdout_encrypted BLOB, stderr_encrypted BLOB, result_digest TEXT NOT NULL DEFAULT '', result_json TEXT NOT NULL DEFAULT '{}',
		retry_actor TEXT NOT NULL DEFAULT '', retry_reason TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL, completed_at INTEGER, UNIQUE(target_id,attempt_number))`,
	`CREATE INDEX IF NOT EXISTS idx_operation_targets_dispatch ON operation_task_targets(node_id,state,created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_operation_tasks_created ON operation_tasks(created_at DESC,id)`,
	`CREATE INDEX IF NOT EXISTS idx_operation_tasks_policy ON operation_tasks(policy_id,created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_operation_attempts_completed ON operation_attempts(completed_at)`,
	`CREATE TABLE IF NOT EXISTS operation_capabilities (
		node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE, digest TEXT NOT NULL,
		configuration_json TEXT NOT NULL CHECK(length(configuration_json)<=16384), seen_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_artifacts (
		id TEXT PRIMARY KEY, storage_name TEXT NOT NULL UNIQUE, metadata_encrypted BLOB NOT NULL,
		direction TEXT NOT NULL DEFAULT 'deploy' CHECK(direction IN ('deploy','collect')),
		byte_size INTEGER NOT NULL CHECK(byte_size BETWEEN 0 AND 2147483648), digest TEXT NOT NULL,
		scan_status TEXT NOT NULL CHECK(scan_status IN ('pending','clean','rejected','unavailable')),
		state TEXT NOT NULL CHECK(state IN ('awaiting_source','uploading','scanning','ready','canceled','expired','corrupt')),
		actor_id TEXT NOT NULL, scan_id TEXT NOT NULL DEFAULT '', scan_deadline INTEGER NOT NULL DEFAULT 0, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_transfers (
		id TEXT PRIMARY KEY, policy_id TEXT REFERENCES operation_policies(id),task_id TEXT REFERENCES operation_tasks(id),require_scan INTEGER NOT NULL DEFAULT 0 CHECK(require_scan IN (0,1)),
		direction TEXT NOT NULL CHECK(direction IN ('deploy','collect')), path_encrypted BLOB NOT NULL,
		root_id TEXT NOT NULL, byte_size INTEGER NOT NULL CHECK(byte_size BETWEEN 0 AND 2147483648), digest TEXT NOT NULL,
		mode INTEGER NOT NULL CHECK(mode IN (384,448)), overwrite INTEGER NOT NULL DEFAULT 0 CHECK(overwrite IN (0,1)),
		artifact_id TEXT REFERENCES operation_artifacts(id), state TEXT NOT NULL, actor_id TEXT NOT NULL,
		expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS operation_transfer_targets (
		id TEXT PRIMARY KEY, transfer_id TEXT NOT NULL REFERENCES operation_transfers(id), node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
		node_name TEXT NOT NULL, attempt_id TEXT, progress_bytes INTEGER NOT NULL DEFAULT 0 CHECK(progress_bytes>=0),
		state TEXT NOT NULL, result_json TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		UNIQUE(transfer_id,node_id))`,
	`CREATE TABLE IF NOT EXISTS operation_artifact_chunks (
 id TEXT PRIMARY KEY,artifact_id TEXT NOT NULL REFERENCES operation_artifacts(id),chunk_index INTEGER NOT NULL CHECK(chunk_index BETWEEN 0 AND 4095),
 storage_name TEXT NOT NULL UNIQUE,data_key_id TEXT NOT NULL REFERENCES application_data_keys(id),
 byte_size INTEGER NOT NULL CHECK(byte_size BETWEEN 0 AND 4194304),digest TEXT NOT NULL,
 created_at INTEGER NOT NULL, UNIQUE(artifact_id,chunk_index))`,
	`CREATE TRIGGER IF NOT EXISTS operation_node_delete_guard BEFORE DELETE ON nodes WHEN
 EXISTS(SELECT 1 FROM operation_task_targets WHERE node_id=OLD.id AND state IN ('queued','leased','running','unknown','cancel_requested')) OR
 EXISTS(SELECT 1 FROM operation_transfer_targets WHERE node_id=OLD.id AND state IN ('queued','leased','running','unknown','cancel_requested','uploading'))
 BEGIN SELECT RAISE(ABORT,'operation_node_busy'); END`,
	`CREATE TRIGGER IF NOT EXISTS operation_group_delete_guard BEFORE DELETE ON groups WHEN
 EXISTS(SELECT 1 FROM operation_policy_groups g JOIN operation_policies p ON p.id=g.policy_id WHERE g.group_id=OLD.id AND p.enabled=1)
 BEGIN SELECT RAISE(ABORT,'operation_group_busy'); END`,
}

var operationLifecycleSchema = []string{
	`ALTER TABLE application_secret_state RENAME TO application_secret_state_v10`,
	`CREATE TABLE application_secret_state (
 id INTEGER PRIMARY KEY CHECK(id=1), phase TEXT NOT NULL CHECK(phase IN ('pending','backfill','sanitize','ready','rotating')),
 generation INTEGER NOT NULL CHECK(generation>=0), root_generation INTEGER NOT NULL CHECK(root_generation>=0),
 root_fingerprint TEXT NOT NULL, active_key_id TEXT NOT NULL,
 cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 12), cursor_id TEXT NOT NULL,
 rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id='' OR length(rotation_id)=36), updated_at INTEGER NOT NULL)`,
	`INSERT INTO application_secret_state SELECT * FROM application_secret_state_v10`,
	`DROP TABLE application_secret_state_v10`,
}
