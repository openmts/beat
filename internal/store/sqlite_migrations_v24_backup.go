package store

import (
	"context"
	"fmt"
)

// MigrateBackupLifecycle 属于 v24 同一批次：扩展 admin_backups 的生命周期状态与
// 恢复代次列，并登记孤儿归档隔离清单。
//
// 迁移整体运行在同一事务里，因此重建表要么全部生效要么全部回滚；重复执行时按
// restore_generation 列是否存在判断重建是否已完成。
func MigrateBackupLifecycle(ctx context.Context, db schemaExecutor) error {
	var rebuilt int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('admin_backups') WHERE name = 'restore_generation'",
	).Scan(&rebuilt); err != nil {
		return fmt.Errorf("inspect admin_backups lifecycle columns: %w", err)
	}
	if rebuilt == 0 {
		if err := rebuildAdminBackups(ctx, db); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_admin_backups_created ON admin_backups(created_at DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS admin_backup_quarantine (
			id TEXT PRIMARY KEY,
			filename TEXT NOT NULL UNIQUE,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 32),
			quarantined_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply backup lifecycle schema: %w", err)
		}
	}
	return nil
}

// rebuildAdminBackups 用最终约束重建历史表，保留既有行并把 updated_at 初始化为 created_at。
func rebuildAdminBackups(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`CREATE TABLE admin_backups_lifecycle (
			id TEXT PRIMARY KEY,
			filename TEXT NOT NULL UNIQUE,
			source TEXT NOT NULL CHECK(source IN ('generated', 'uploaded')),
			state TEXT NOT NULL CHECK(state IN ('running', 'ready', 'failed', 'validated',
				'staging', 'staged', 'interrupted', 'missing', 'deleting')),
			created_at DATETIME NOT NULL,
			completed_at DATETIME,
			snapshot_cutoff DATETIME,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			sqlite_bytes INTEGER NOT NULL DEFAULT 0,
			metrics_bytes INTEGER NOT NULL DEFAULT 0,
			metric_rows INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			restore_generation TEXT NOT NULL DEFAULT '',
			previous_state TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL,
			reconciled_at DATETIME
		)`,
		`INSERT INTO admin_backups_lifecycle (id, filename, source, state, created_at,
			completed_at, snapshot_cutoff, size_bytes, sqlite_bytes, metrics_bytes,
			metric_rows, error_message, restore_generation, previous_state, updated_at,
			reconciled_at)
			SELECT id, filename, source, state, created_at, completed_at, snapshot_cutoff,
				size_bytes, sqlite_bytes, metrics_bytes, metric_rows, error_message,
				'', '', created_at, NULL FROM admin_backups`,
		`DROP TABLE admin_backups`,
		`ALTER TABLE admin_backups_lifecycle RENAME TO admin_backups`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("rebuild admin_backups lifecycle: %w", err)
		}
	}
	return nil
}
