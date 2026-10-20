package store

import "context"

func applyAgentEnrollment(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS agent_enrollment_tokens (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
			token_prefix TEXT NOT NULL,
			server_url TEXT NOT NULL,
			allow_http INTEGER NOT NULL CHECK(allow_http IN (0, 1)),
			creator_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			revoked_at DATETIME,
			consumed_at DATETIME,
			credential_created_at DATETIME,
			request_id TEXT NOT NULL DEFAULT '',
			node_id TEXT,
			agent_token_hash BLOB CHECK(agent_token_hash IS NULL OR length(agent_token_hash) = 32),
			node_name TEXT NOT NULL DEFAULT '',
			advertised_host TEXT NOT NULL DEFAULT '',
			ssh_port INTEGER NOT NULL DEFAULT 22 CHECK(ssh_port BETWEEN 1 AND 65535),
			consumed_ip TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_enrollment_expiry ON agent_enrollment_tokens(expires_at)`,
		`CREATE TABLE IF NOT EXISTS agent_enrollment_identities (
			agent_token_hash BLOB PRIMARY KEY CHECK(length(agent_token_hash) = 32),
			node_id TEXT,
			credential_created_at DATETIME NOT NULL,
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE SET NULL
		)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
