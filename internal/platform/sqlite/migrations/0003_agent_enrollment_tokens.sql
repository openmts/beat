CREATE TABLE IF NOT EXISTS agent_enrollment_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    bound_agent_id TEXT,
    bound_node_id TEXT,
    revoked_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_agent_enrollment_tokens_expires_at ON agent_enrollment_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_agent_enrollment_tokens_bound_agent ON agent_enrollment_tokens(bound_agent_id);
