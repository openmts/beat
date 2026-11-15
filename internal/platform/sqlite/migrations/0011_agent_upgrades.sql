CREATE TABLE IF NOT EXISTS agent_upgrade_batches (
    id TEXT PRIMARY KEY,
    target_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    total_items INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_agent_upgrade_batches_created_at
    ON agent_upgrade_batches(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_upgrade_batches_status
    ON agent_upgrade_batches(status);

CREATE TABLE IF NOT EXISTS agent_upgrade_items (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    node_name TEXT NOT NULL DEFAULT '',
    arch TEXT NOT NULL DEFAULT '',
    from_version TEXT NOT NULL DEFAULT '',
    to_version TEXT NOT NULL,
    artifact_sha256 TEXT NOT NULL DEFAULT '',
    artifact_size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued',
    message TEXT NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    delivered_at INTEGER,
    completed_at INTEGER,
    FOREIGN KEY (batch_id) REFERENCES agent_upgrade_batches(id) ON DELETE CASCADE,
    UNIQUE(batch_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_upgrade_items_batch_status
    ON agent_upgrade_items(batch_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_upgrade_items_agent
    ON agent_upgrade_items(agent_id, created_at DESC);
