package store

import "context"

// MigrateApplicationSecrets 仅执行 v9 DDL；密钥加载与分批转换在监听前完成。
func MigrateApplicationSecrets(ctx context.Context, db schemaExecutor) error {
	return executeSchemaStatements(ctx, db, applicationSecretSchema)
}

var applicationSecretSchema = []string{
	`CREATE TABLE application_data_keys (
		id TEXT PRIMARY KEY CHECK(length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*' AND id <> '00000000000000000000000000000000'),
		wrapped_key BLOB NOT NULL CHECK(length(wrapped_key) = 90),
		generation INTEGER NOT NULL CHECK(generation > 0),
		state TEXT NOT NULL CHECK(state IN ('active', 'decrypt_only')),
		created_at INTEGER NOT NULL,
		activated_at INTEGER NOT NULL,
		retire_after INTEGER,
		CHECK(state <> 'active' OR retire_after IS NULL)
	)`,
	`CREATE UNIQUE INDEX application_data_key_active ON application_data_keys(state) WHERE state = 'active'`,
	`CREATE TABLE application_secret_state (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		phase TEXT NOT NULL CHECK(phase IN ('pending', 'backfill', 'sanitize', 'ready', 'rotating')),
		generation INTEGER NOT NULL CHECK(generation >= 0),
		root_generation INTEGER NOT NULL CHECK(root_generation >= 0),
		root_fingerprint TEXT NOT NULL,
		active_key_id TEXT NOT NULL,
		cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 4),
		cursor_id TEXT NOT NULL,
		rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id = '' OR length(rotation_id) = 36),
		updated_at INTEGER NOT NULL
	)`,
	`INSERT INTO application_secret_state
		(id, phase, generation, root_generation, root_fingerprint, active_key_id, cursor_source, cursor_id, updated_at)
		VALUES (1, 'pending', 0, 0, '', '', 0, '', 0)`,
	`ALTER TABLE ssh_keys ADD COLUMN private_key_encrypted BLOB`,
}
