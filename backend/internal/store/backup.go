package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/beat/backend/internal/model"
)

const backupColumns = `id, filename, source, state, created_at, completed_at,
	snapshot_cutoff, size_bytes, sqlite_bytes, metrics_bytes, metric_rows, error_message`

type BackupStore struct {
	db *sql.DB
}

func NewBackupStore(db *sql.DB) *BackupStore {
	return &BackupStore{db: db}
}

func (store *BackupStore) Create(ctx context.Context, record *model.BackupRecord) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_backups (`+backupColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, backupArgs(record)...)
	if err != nil {
		return fmt.Errorf("create backup record: %w", err)
	}
	return nil
}

func (store *BackupStore) Update(ctx context.Context, record *model.BackupRecord) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_backups SET filename = ?, source = ?,
		state = ?, created_at = ?, completed_at = ?, snapshot_cutoff = ?, size_bytes = ?,
		sqlite_bytes = ?, metrics_bytes = ?, metric_rows = ?, error_message = ? WHERE id = ?`,
		record.Filename, record.Source, record.State, record.CreatedAt, record.CompletedAt,
		record.SnapshotCutoff, record.SizeBytes, record.SQLiteBytes, record.MetricsBytes,
		record.MetricRows, record.ErrorMessage, record.ID)
	if err != nil {
		return fmt.Errorf("update backup record: %w", err)
	}
	return requireAffected(result, "backup record")
}

func (store *BackupStore) Get(ctx context.Context, id string) (*model.BackupRecord, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM admin_backups WHERE id = ?", id)
	record, err := scanBackup(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get backup record: %w", err)
	}
	return &record, nil
}

func (store *BackupStore) List(ctx context.Context) ([]model.BackupRecord, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+backupColumns+" FROM admin_backups ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("list backup records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records := []model.BackupRecord{}
	for rows.Next() {
		record, scanErr := scanBackup(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan backup record: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup records: %w", err)
	}
	return records, nil
}

func (store *BackupStore) Delete(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, "DELETE FROM admin_backups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete backup record: %w", err)
	}
	return requireAffected(result, "backup record")
}

type backupScanner interface {
	Scan(...any) error
}

func scanBackup(scanner backupScanner) (model.BackupRecord, error) {
	var record model.BackupRecord
	err := scanner.Scan(&record.ID, &record.Filename, &record.Source, &record.State,
		&record.CreatedAt, &record.CompletedAt, &record.SnapshotCutoff, &record.SizeBytes,
		&record.SQLiteBytes, &record.MetricsBytes, &record.MetricRows, &record.ErrorMessage)
	return record, err
}

func backupArgs(record *model.BackupRecord) []any {
	return []any{record.ID, record.Filename, record.Source, record.State, record.CreatedAt,
		record.CompletedAt, record.SnapshotCutoff, record.SizeBytes, record.SQLiteBytes,
		record.MetricsBytes, record.MetricRows, record.ErrorMessage}
}

func requireAffected(result sql.Result, resource string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s affected rows: %w", resource, err)
	}
	if count == 0 {
		return fmt.Errorf("%s not found", resource)
	}
	return nil
}
