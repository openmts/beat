package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

const backupQuarantineColumns = `id, filename, size_bytes, reason, quarantined_at`

// RecordQuarantine 登记被移入私有隔离目录的孤儿归档；同名归档只登记一次。
// 归档本身既不自动导入也不自动删除，只在这里留下可审计清单。
func (store *BackupStore) RecordQuarantine(
	ctx context.Context,
	record *model.BackupQuarantineRecord,
) error {
	_, err := store.db.ExecContext(ctx, `INSERT OR IGNORE INTO admin_backup_quarantine (`+
		backupQuarantineColumns+`) VALUES (?, ?, ?, ?, ?)`, record.ID, record.Filename,
		record.SizeBytes, record.Reason, record.QuarantinedAt)
	if err != nil {
		return fmt.Errorf("record quarantined backup: %w", err)
	}
	return nil
}

// CountQuarantine 返回隔离清单条目数，用于运维只读指标。
func (store *BackupStore) CountQuarantine(ctx context.Context) (int, error) {
	var count int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM admin_backup_quarantine").Scan(&count); err != nil {
		return 0, fmt.Errorf("count quarantined backups: %w", err)
	}
	return count, nil
}

// ListQuarantine 返回有界的隔离清单，只暴露文件名、大小、原因与时间。
func (store *BackupStore) ListQuarantine(
	ctx context.Context,
	limit int,
) ([]model.BackupQuarantineRecord, error) {
	if limit < 1 || limit > BackupMaximumPage {
		return nil, ErrInvalidBackupQuery
	}
	rows, err := store.db.QueryContext(ctx, "SELECT "+backupQuarantineColumns+
		" FROM admin_backup_quarantine ORDER BY quarantined_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list quarantined backups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records := []model.BackupQuarantineRecord{}
	for rows.Next() {
		var record model.BackupQuarantineRecord
		if err := rows.Scan(&record.ID, &record.Filename, &record.SizeBytes, &record.Reason,
			&record.QuarantinedAt); err != nil {
			return nil, fmt.Errorf("scan quarantined backup: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quarantined backups: %w", err)
	}
	return records, nil
}
