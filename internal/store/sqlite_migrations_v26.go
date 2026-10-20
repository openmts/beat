package store

import "context"

// applyAgentDiscovery 引入 v26 自动发现式入网：授权、挑战、请求三张表，外加节点显式凭据状态。
// 迁移只做可加列与回填，既有节点按 token 哈希与撤销时间确定性地落到 legacy/active/revoked。
func applyAgentDiscovery(ctx context.Context, db schemaExecutor) error {
	for _, statement := range agentDiscoverySchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

var agentDiscoverySchema = []string{
	`CREATE TABLE IF NOT EXISTS agent_enrollment_grants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE CHECK(length(name) BETWEEN 1 AND 200),
		secret_hash BLOB NOT NULL UNIQUE CHECK(length(secret_hash) = 32),
		secret_prefix TEXT NOT NULL CHECK(length(secret_prefix) BETWEEN 1 AND 64),
		approval_mode TEXT NOT NULL CHECK(approval_mode IN ('manual', 'automatic')),
		enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
		expires_at DATETIME NOT NULL,
		revoked_at DATETIME,
		default_group_id TEXT NOT NULL REFERENCES groups(id),
		node_name_prefix TEXT NOT NULL DEFAULT '' CHECK(length(CAST(node_name_prefix AS BLOB)) <= 256),
		tags_json TEXT NOT NULL CHECK(json_valid(tags_json) AND length(tags_json) <= 1024),
		remarks TEXT NOT NULL DEFAULT '' CHECK(length(CAST(remarks AS BLOB)) <= 8000),
		is_public INTEGER NOT NULL CHECK(is_public IN (0, 1)),
		ssh_port INTEGER NOT NULL CHECK(ssh_port BETWEEN 1 AND 65535),
		source_ip_as_host INTEGER NOT NULL CHECK(source_ip_as_host IN (0, 1)),
		source_cidrs_json TEXT NOT NULL CHECK(json_valid(source_cidrs_json) AND length(source_cidrs_json) <= 1024),
		hostname_pattern TEXT NOT NULL DEFAULT '' CHECK(length(CAST(hostname_pattern AS BLOB)) <= 256),
		claim_recovery INTEGER NOT NULL CHECK(claim_recovery IN (0, 1)),
		maximum_enrollments INTEGER NOT NULL CHECK(maximum_enrollments BETWEEN 1 AND 1000),
		consumed_enrollments INTEGER NOT NULL DEFAULT 0 CHECK(consumed_enrollments >= 0
			AND consumed_enrollments <= maximum_enrollments),
		created_by TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_enrollment_grants_status
		ON agent_enrollment_grants(enabled, expires_at)`,
	`CREATE TABLE IF NOT EXISTS agent_enrollment_challenges (
		id TEXT PRIMARY KEY,
		grant_id TEXT NOT NULL REFERENCES agent_enrollment_grants(id) ON DELETE CASCADE,
		challenge_hash BLOB NOT NULL UNIQUE CHECK(length(challenge_hash) = 32),
		key_fingerprint BLOB NOT NULL CHECK(length(key_fingerprint) = 32),
		metadata_digest BLOB NOT NULL CHECK(length(metadata_digest) = 32),
		source_ip TEXT NOT NULL CHECK(length(source_ip) BETWEEN 1 AND 64),
		expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, consumed_at DATETIME)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_enrollment_challenges_expiry
		ON agent_enrollment_challenges(grant_id, expires_at)`,
	`CREATE TABLE IF NOT EXISTS agent_enrollment_requests (
		id TEXT PRIMARY KEY,
		grant_id TEXT NOT NULL REFERENCES agent_enrollment_grants(id),
		key_fingerprint BLOB NOT NULL CHECK(length(key_fingerprint) = 32),
		public_key BLOB NOT NULL CHECK(length(public_key) = 32),
		token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
		token_prefix TEXT NOT NULL CHECK(length(token_prefix) BETWEEN 1 AND 64),
		token_rotated_at DATETIME NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('pending', 'approved', 'rejected', 'expired', 'claimed')),
		hostname TEXT NOT NULL DEFAULT '' CHECK(length(CAST(hostname AS BLOB)) <= 256),
		advertised_host TEXT NOT NULL DEFAULT '' CHECK(length(CAST(advertised_host AS BLOB)) <= 256),
		os TEXT NOT NULL DEFAULT '' CHECK(length(CAST(os AS BLOB)) <= 128),
		architecture TEXT NOT NULL DEFAULT '' CHECK(length(CAST(architecture AS BLOB)) <= 128),
		agent_version TEXT NOT NULL DEFAULT '' CHECK(length(CAST(agent_version AS BLOB)) <= 128),
		virtualization TEXT NOT NULL DEFAULT '' CHECK(length(CAST(virtualization AS BLOB)) <= 256),
		ssh_port INTEGER NOT NULL CHECK(ssh_port BETWEEN 1 AND 65535),
		installation_nonce TEXT NOT NULL CHECK(length(CAST(installation_nonce AS BLOB)) BETWEEN 1 AND 128),
		source_ip TEXT NOT NULL CHECK(length(source_ip) BETWEEN 1 AND 64),
		first_request_at DATETIME NOT NULL, last_request_at DATETIME NOT NULL,
		reserved_node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
		node_name_snapshot TEXT NOT NULL DEFAULT '' CHECK(length(CAST(node_name_snapshot AS BLOB)) <= 256),
		approved_by TEXT NOT NULL DEFAULT '', approved_at DATETIME,
		rejected_by TEXT NOT NULL DEFAULT '', rejected_at DATETIME,
		rejection_reason TEXT NOT NULL DEFAULT '' CHECK(length(rejection_reason) <= 64),
		claimed_at DATETIME,
		ticket_hash BLOB CHECK(ticket_hash IS NULL OR length(ticket_hash) = 32),
		ticket_generation INTEGER NOT NULL DEFAULT 0 CHECK(ticket_generation >= 0),
		ticket_expires_at DATETIME,
		recovery_deadline DATETIME,
		recovery_attempts INTEGER NOT NULL DEFAULT 0 CHECK(recovery_attempts BETWEEN 0 AND 3),
		request_expires_at DATETIME NOT NULL)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_enrollment_requests_open
		ON agent_enrollment_requests(grant_id, key_fingerprint)
		WHERE status IN ('pending', 'approved')`,
	`CREATE INDEX IF NOT EXISTS idx_agent_enrollment_requests_status
		ON agent_enrollment_requests(status, last_request_at)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_enrollment_requests_reserved
		ON agent_enrollment_requests(reserved_node_id)`,
	`ALTER TABLE nodes ADD COLUMN credential_state TEXT NOT NULL DEFAULT 'legacy'
		CHECK(credential_state IN ('legacy', 'pending_claim', 'active', 'revoked'))`,
	`UPDATE nodes SET credential_state = CASE
		WHEN agent_token_revoked_at IS NOT NULL THEN 'revoked'
		WHEN agent_token_hash IS NOT NULL THEN 'active'
		ELSE 'legacy' END`,
	`CREATE INDEX IF NOT EXISTS idx_nodes_credential_state ON nodes(credential_state)`,
}
