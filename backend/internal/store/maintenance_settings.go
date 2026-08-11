package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const interruptedMaintenanceMessage = "maintenance interrupted by server restart"

type MaintenanceSettingsStore struct {
	db *sql.DB
}

func NewMaintenanceSettingsStore(db *sql.DB) *MaintenanceSettingsStore {
	return &MaintenanceSettingsStore{db: db}
}

func (store *MaintenanceSettingsStore) Get(
	ctx context.Context,
) (model.MaintenanceSettings, model.MaintenanceRunStatus, error) {
	var settings model.MaintenanceSettings
	var status model.MaintenanceRunStatus
	var automatic, running int
	err := store.db.QueryRowContext(ctx, maintenanceSettingsQuery).Scan(
		&settings.RetentionDays, &automatic, &settings.CleanupHourUTC, &settings.UpdatedAt,
		&running, &status.LastStartedAt, &status.LastCompletedAt, &status.LastStatus,
		&status.LastError, &status.LastCutoffAt, &status.LastDurationMS,
		&status.LastTrigger, &status.SQLiteIntegrity,
	)
	if err != nil {
		return settings, status, fmt.Errorf("get maintenance settings: %w", err)
	}
	settings.AutoCleanupEnabled = automatic == 1
	status.Running = running == 1
	return settings, status, nil
}

func (store *MaintenanceSettingsStore) Update(
	ctx context.Context,
	settings model.MaintenanceSettings,
) (model.MaintenanceSettings, error) {
	if err := settings.Validate(); err != nil {
		return model.MaintenanceSettings{}, err
	}
	settings.UpdatedAt = model.NowUTC()
	_, err := store.db.ExecContext(ctx, `UPDATE maintenance_settings SET
		retention_days = ?, auto_cleanup_enabled = ?, cleanup_hour_utc = ?, updated_at = ?
		WHERE id = 1`, settings.RetentionDays, settings.AutoCleanupEnabled,
		settings.CleanupHourUTC, settings.UpdatedAt)
	if err != nil {
		return model.MaintenanceSettings{}, fmt.Errorf("update maintenance settings: %w", err)
	}
	return settings, nil
}

func (store *MaintenanceSettingsStore) MarkStarted(
	ctx context.Context,
	trigger string,
	startedAt time.Time,
	cutoff time.Time,
) error {
	_, err := store.db.ExecContext(ctx, `UPDATE maintenance_settings SET running = 1,
		last_started_at = ?, last_completed_at = NULL, last_status = ?, last_error = '',
		last_cutoff_at = ?, last_duration_ms = 0, last_trigger = ?, sqlite_integrity = ''
		WHERE id = 1`, startedAt, model.MaintenanceStatusRunning, cutoff, trigger)
	if err != nil {
		return fmt.Errorf("mark maintenance started: %w", err)
	}
	return nil
}

func (store *MaintenanceSettingsStore) MarkCompleted(
	ctx context.Context,
	completedAt time.Time,
	duration time.Duration,
	result string,
	message string,
	integrity string,
) error {
	_, err := store.db.ExecContext(ctx, `UPDATE maintenance_settings SET running = 0,
		last_completed_at = ?, last_status = ?, last_error = ?, last_duration_ms = ?,
		sqlite_integrity = ? WHERE id = 1`, completedAt, result, message,
		duration.Milliseconds(), integrity)
	if err != nil {
		return fmt.Errorf("mark maintenance completed: %w", err)
	}
	return nil
}

func (store *MaintenanceSettingsStore) RecoverInterrupted(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `UPDATE maintenance_settings SET running = 0,
		last_completed_at = CURRENT_TIMESTAMP, last_status = ?, last_error = ?
		WHERE id = 1 AND running = 1`, model.MaintenanceStatusFailed,
		interruptedMaintenanceMessage)
	if err != nil {
		return fmt.Errorf("recover interrupted maintenance: %w", err)
	}
	return nil
}

const maintenanceSettingsQuery = `SELECT retention_days, auto_cleanup_enabled,
	cleanup_hour_utc, updated_at, running, last_started_at, last_completed_at,
	last_status, last_error, last_cutoff_at, last_duration_ms, last_trigger,
	sqlite_integrity FROM maintenance_settings WHERE id = 1`
