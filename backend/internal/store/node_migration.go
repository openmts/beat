package store

import (
	"context"
	"fmt"
)

var nodeColumns = map[string]string{
	"cpu_model":                "TEXT NOT NULL DEFAULT ''",
	"os":                       "TEXT NOT NULL DEFAULT ''",
	"platform":                 "TEXT NOT NULL DEFAULT ''",
	"os_version":               "TEXT NOT NULL DEFAULT ''",
	"kernel":                   "TEXT NOT NULL DEFAULT ''",
	"arch":                     "TEXT NOT NULL DEFAULT ''",
	"virtualization":           "TEXT NOT NULL DEFAULT ''",
	"agent_version":            "TEXT NOT NULL DEFAULT ''",
	"sort_order":               "INTEGER NOT NULL DEFAULT 0 CHECK(sort_order >= 0)",
	"tags":                     "TEXT NOT NULL DEFAULT '[]'",
	"is_public":                "INTEGER NOT NULL DEFAULT 1 CHECK(is_public IN (0, 1))",
	"public_remark":            "TEXT NOT NULL DEFAULT ''",
	"private_remark":           "TEXT NOT NULL DEFAULT ''",
	"agent_token_hash":         "BLOB",
	"agent_token_prefix":       "TEXT NOT NULL DEFAULT ''",
	"agent_token_created_at":   "DATETIME",
	"agent_token_last_used_at": "DATETIME",
	"agent_token_revoked_at":   "DATETIME",
	"traffic_limit":            "INTEGER NOT NULL DEFAULT 0",
	"traffic_limit_type":       "TEXT NOT NULL DEFAULT 'sum'",
	"traffic_reset_day":        "INTEGER NOT NULL DEFAULT 1",
}

func ensureNodeIndexes(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_name_unique ON nodes(name)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_single_default ON groups(is_default) WHERE is_default = 1",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_agent_token_hash_unique " +
			"ON nodes(agent_token_hash) WHERE agent_token_hash IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_nodes_public_group_order " +
			"ON nodes(is_public, group_id, sort_order, name)",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create node index: %w", err)
		}
	}
	return nil
}

func ensureNodeColumns(ctx context.Context, db schemaExecutor) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(nodes)")
	if err != nil {
		return fmt.Errorf("query node columns: %w", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan node column: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close node columns: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate node columns: %w", err)
	}
	for name, definition := range nodeColumns {
		if existing[name] {
			continue
		}
		statement := fmt.Sprintf("ALTER TABLE nodes ADD COLUMN %s %s", name, definition)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add node column %s: %w", name, err)
		}
	}
	return nil
}
