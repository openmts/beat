CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL,
    address TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    kernel TEXT NOT NULL DEFAULT '',
    arch TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    labels_json TEXT NOT NULL DEFAULT '{}',
    interfaces_json TEXT NOT NULL DEFAULT '[]',
    cpu_cores INTEGER NOT NULL DEFAULT 0,
    cpu_model TEXT NOT NULL DEFAULT '',
    total_memory_bytes INTEGER NOT NULL DEFAULT 0,
    total_disk_bytes INTEGER NOT NULL DEFAULT 0,
    collector_interval_seconds INTEGER NOT NULL DEFAULT 15,
    latest_metrics_json TEXT NOT NULL DEFAULT '{}',
    first_seen INTEGER NOT NULL,
    last_seen INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
CREATE INDEX IF NOT EXISTS idx_nodes_last_seen ON nodes(last_seen);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    interval_seconds INTEGER NOT NULL DEFAULT 15,
    last_seen INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

