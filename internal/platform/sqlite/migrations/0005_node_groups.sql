CREATE TABLE IF NOT EXISTS node_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT 'blue',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS node_group_members (
    node_id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL,
    assigned_at INTEGER NOT NULL,
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (group_id) REFERENCES node_groups(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_node_group_members_group_id ON node_group_members(group_id);
