package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ValidateSnapshotSchema 在恢复替换数据前只读检查迁移历史，不执行迁移。
func ValidateSnapshotSchema(ctx context.Context, database *sql.DB) (resultErr error) {
	rows, err := database.QueryContext(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read SQLite snapshot schema: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	previous := 0
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return fmt.Errorf("read SQLite schema entry: %w", err)
		}
		if version < 1 || version > currentSchemaVersion {
			return fmt.Errorf("unsupported SQLite schema version %d (maximum %d)", version, currentSchemaVersion)
		}
		if version != previous+1 || name != sqliteMigrations[version-1].name {
			return errors.New("SQLite schema history is incomplete or incompatible")
		}
		previous = version
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate SQLite schema history: %w", err)
	}
	if previous == 0 {
		return errors.New("SQLite schema history is empty")
	}
	return nil
}
