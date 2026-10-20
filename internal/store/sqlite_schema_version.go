package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SchemaVersion 读取已应用迁移的最大版本；只读查询，不修改数据库或账本。
func (s *SQLiteStore) SchemaVersion(ctx context.Context) (int, error) {
	var version sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("query sqlite schema version: %w", err)
	}
	if !version.Valid {
		return 0, errors.New("sqlite schema history is empty")
	}
	return int(version.Int64), nil
}

// MigrationName 返回账本版本对应的规范迁移名；未知版本返回空字符串。
func MigrationName(version int) string {
	inventory := MigrationInventory()
	if version < 1 || version > len(inventory) {
		return ""
	}
	return inventory[version-1].Name
}
