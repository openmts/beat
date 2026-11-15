CREATE TABLE IF NOT EXISTS task_template_configs (
    task_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'shell_script',
    config_json TEXT NOT NULL DEFAULT '{}',
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES task_definitions(id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO task_template_configs (task_id, kind, config_json, updated_at)
SELECT id, 'shell_script', '{}', updated_at FROM task_definitions;
