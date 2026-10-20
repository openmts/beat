package store

import "context"

// MigrateAgentRollout 由中央 v15 迁移事务登记签名发布及升级状态。
func MigrateAgentRollout(ctx context.Context, db schemaExecutor) error {
	for _, statement := range agentRolloutSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

var agentRolloutSchema = []string{
	`CREATE TABLE agent_release_signature_policies(revision TEXT PRIMARY KEY,root_sha256 TEXT NOT NULL,policy_json BLOB NOT NULL,created_at DATETIME NOT NULL)`,
	`CREATE TABLE agent_release_signature_state(id INTEGER PRIMARY KEY CHECK(id=1),policy_revision TEXT NOT NULL REFERENCES agent_release_signature_policies(revision),updated_at DATETIME NOT NULL)`,
	`CREATE TABLE agent_release_trust(key_id TEXT PRIMARY KEY,public_key BLOB NOT NULL,revision INTEGER NOT NULL DEFAULT 1,source_key TEXT NOT NULL DEFAULT '',not_before DATETIME NOT NULL,expires_at DATETIME NOT NULL,revoked_at DATETIME,created_at DATETIME NOT NULL)`,
	`CREATE TABLE agent_releases(id TEXT PRIMARY KEY,version TEXT NOT NULL,channel TEXT NOT NULL,signing_key_id TEXT NOT NULL REFERENCES agent_release_trust(key_id),payload BLOB NOT NULL,signature BLOB NOT NULL,sigstore_bundle BLOB NOT NULL,expires_at DATETIME NOT NULL,trust_revision INTEGER NOT NULL DEFAULT 1,signature_policy_revision TEXT NOT NULL,verified INTEGER NOT NULL DEFAULT 0 CHECK(verified IN(0,1)),revoked_at DATETIME,created_by TEXT NOT NULL,created_at DATETIME NOT NULL,UNIQUE(version,channel))`,
	`CREATE TABLE agent_release_artifacts(release_id TEXT NOT NULL REFERENCES agent_releases(id),artifact_id TEXT NOT NULL,digest TEXT NOT NULL,size INTEGER NOT NULL CHECK(size>0),state TEXT NOT NULL CHECK(state IN('missing','ready')),updated_at DATETIME NOT NULL,PRIMARY KEY(release_id,artifact_id))`,
	`CREATE TABLE agent_rollouts(action TEXT NOT NULL DEFAULT 'upgrade' CHECK(action IN('upgrade','rollback')),id TEXT PRIMARY KEY,release_id TEXT NOT NULL REFERENCES agent_releases(id),name TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN('paused','active','completed','aborted')),reason TEXT NOT NULL,policy_json TEXT NOT NULL,current_wave INTEGER NOT NULL DEFAULT -1,total_waves INTEGER NOT NULL,created_by TEXT NOT NULL,created_at DATETIME NOT NULL,updated_at DATETIME NOT NULL)`,
	`CREATE TABLE agent_rollout_targets(id TEXT PRIMARY KEY,rollout_id TEXT NOT NULL REFERENCES agent_rollouts(id),node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,node_snapshot_id TEXT NOT NULL,node_name TEXT NOT NULL,source_version TEXT NOT NULL,os TEXT NOT NULL,arch TEXT NOT NULL,wave INTEGER NOT NULL,state TEXT NOT NULL CHECK(state IN('pending','held','offered','observing','healthy','failed','aborted')),reason TEXT NOT NULL,current_attempt_id TEXT NOT NULL DEFAULT '',artifact_id TEXT NOT NULL DEFAULT '',nonce_hash BLOB,lease_expires_at DATETIME,healthy_since DATETIME,last_report_at DATETIME,observation_json TEXT NOT NULL DEFAULT '{}',updated_at DATETIME NOT NULL,UNIQUE(rollout_id,node_snapshot_id))`,
	`CREATE TABLE agent_rollout_attempts(id TEXT PRIMARY KEY,target_id TEXT NOT NULL REFERENCES agent_rollout_targets(id),nonce_hash BLOB NOT NULL,state TEXT NOT NULL,observation_json TEXT NOT NULL DEFAULT '{}',created_at DATETIME NOT NULL,ended_at DATETIME)`,
	`CREATE UNIQUE INDEX idx_agent_rollout_active_node ON agent_rollout_targets(node_id) WHERE node_id IS NOT NULL AND state IN('offered','observing')`,
	`CREATE INDEX idx_agent_rollout_expiry ON agent_rollout_targets(state,lease_expires_at)`,
	`CREATE INDEX idx_agent_rollout_target_node ON agent_rollout_targets(node_id,state)`,
	`CREATE TABLE agent_rollout_events(id TEXT PRIMARY KEY,rollout_id TEXT NOT NULL,target_id TEXT NOT NULL DEFAULT '',kind TEXT NOT NULL,reason TEXT NOT NULL,created_at DATETIME NOT NULL)`,
}
