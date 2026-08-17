package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const currentSchemaVersion = 3

type sqliteMigration struct {
	version int
	name    string
	apply   func(context.Context, schemaExecutor) error
}

var sqliteMigrations = []sqliteMigration{
	{version: 1, name: "application_schema_baseline", apply: applyApplicationSchema},
	{version: 2, name: "agent_task_channel", apply: applyAgentTaskChannel},
	{version: 3, name: "custom_metric_tasks", apply: applyCustomMetricTasks},
}

func applyCustomMetricTasks(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS custom_metrics (
			id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE CHECK(length(name) BETWEEN 1 AND 100),
			shell_command TEXT NOT NULL CHECK(length(CAST(shell_command AS BLOB)) BETWEEN 1 AND 2048),
			interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 5 AND 86400),
			description TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS custom_metric_nodes (
			metric_id TEXT NOT NULL, node_id TEXT NOT NULL, PRIMARY KEY(metric_id, node_id),
			FOREIGN KEY(metric_id) REFERENCES custom_metrics(id) ON DELETE CASCADE,
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_metric_nodes_node ON custom_metric_nodes(node_id, metric_id)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func applyAgentTaskChannel(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS agent_tasks (
			id TEXT PRIMARY KEY, node_id TEXT NOT NULL, type TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '', status TEXT NOT NULL CHECK(status IN ('pending', 'done', 'failed')),
			created_at DATETIME NOT NULL, attempted_at DATETIME, completed_at DATETIME,
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_tasks_pending ON agent_tasks(node_id, status)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	var maximum sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return fmt.Errorf("query migration version: %w", err)
	}
	if maximum.Valid && maximum.Int64 > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", maximum.Int64, currentSchemaVersion)
	}
	for _, migration := range sqliteMigrations {
		applied, err := migrationApplied(ctx, db, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("query migration %d: %w", version, err)
	}
	return count == 1, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration sqliteMigration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", migration.version, err)
	}
	defer func() { _ = tx.Rollback() }()
	var applied int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version,
	).Scan(&applied); err != nil {
		return fmt.Errorf("recheck migration %d: %w", migration.version, err)
	}
	if applied == 1 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration recheck %d: %w", migration.version, err)
		}
		return nil
	}
	if err := migration.apply(ctx, tx); err != nil {
		return fmt.Errorf("apply migration %d %s: %w", migration.version, migration.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		migration.version, migration.name, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("record migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", migration.version, err)
	}
	return nil
}

func applyApplicationSchema(ctx context.Context, db schemaExecutor) error {
	if err := createTables(ctx, db); err != nil {
		return err
	}
	if err := ensureNodeColumns(ctx, db); err != nil {
		return fmt.Errorf("ensure node columns: %w", err)
	}
	if err := ensureDefaultGroup(ctx, db); err != nil {
		return fmt.Errorf("ensure default group: %w", err)
	}
	if err := ensureNodeIndexes(ctx, db); err != nil {
		return fmt.Errorf("ensure node indexes: %w", err)
	}
	return nil
}
