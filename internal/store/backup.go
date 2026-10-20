package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrBackupStateConflict 表示 CAS 推进失败：记录不存在或状态已被其他变更方改写。
var ErrBackupStateConflict = errors.New("backup record state changed")

const backupColumns = `id, filename, source, state, created_at, completed_at,
	snapshot_cutoff, size_bytes, sqlite_bytes, metrics_bytes, metric_rows, error_message,
	restore_generation, previous_state, updated_at, reconciled_at`

const backupColumnCount = 16

type BackupStore struct {
	db *sql.DB
}

func NewBackupStore(db *sql.DB) *BackupStore {
	return &BackupStore{db: db}
}

func (store *BackupStore) Create(ctx context.Context, record *model.BackupRecord) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_backups (`+backupColumns+`)
		VALUES (`+backupPlaceholders()+`)`, backupArgs(record)...)
	if err != nil {
		return fmt.Errorf("create backup record: %w", err)
	}
	return nil
}

func (store *BackupStore) Update(ctx context.Context, record *model.BackupRecord) error {
	args := append(backupUpdateArgs(record), record.ID)
	result, err := store.db.ExecContext(ctx, `UPDATE admin_backups SET `+backupUpdateSet()+
		` WHERE id = ?`, args...)
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
		&record.SQLiteBytes, &record.MetricsBytes, &record.MetricRows, &record.ErrorMessage,
		&record.RestoreGeneration, &record.PreviousState, &record.UpdatedAt, &record.ReconciledAt)
	return record, err
}

func backupArgs(record *model.BackupRecord) []any {
	return append([]any{record.ID}, backupUpdateArgs(record)...)
}

func backupUpdateArgs(record *model.BackupRecord) []any {
	return []any{record.Filename, record.Source, record.State, record.CreatedAt, record.CompletedAt,
		record.SnapshotCutoff, record.SizeBytes, record.SQLiteBytes, record.MetricsBytes,
		record.MetricRows, record.ErrorMessage, record.RestoreGeneration, record.PreviousState,
		backupUpdatedAt(record), record.ReconciledAt}
}

// backupUpdatedAt 在调用方未显式推进时间戳时回落到创建时间，保证非空约束成立。
func backupUpdatedAt(record *model.BackupRecord) time.Time {
	if record.UpdatedAt.IsZero() {
		return record.CreatedAt
	}
	return record.UpdatedAt
}

func backupUpdateSet() string {
	assignments := []string{"filename = ?", "source = ?", "state = ?", "created_at = ?",
		"completed_at = ?", "snapshot_cutoff = ?", "size_bytes = ?", "sqlite_bytes = ?",
		"metrics_bytes = ?", "metric_rows = ?", "error_message = ?", "restore_generation = ?",
		"previous_state = ?", "updated_at = ?", "reconciled_at = ?"}
	return strings.Join(assignments, ", ")
}

func backupPlaceholders() string {
	placeholders := make([]string, backupColumnCount)
	for index := range placeholders {
		placeholders[index] = "?"
	}
	return strings.Join(placeholders, ", ")
}

func requireStateChange(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read backup record affected rows: %w", err)
	}
	if count == 0 {
		return ErrBackupStateConflict
	}
	return nil
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
