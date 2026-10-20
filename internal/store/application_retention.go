package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const interruptedApplicationRetentionCode = "interrupted"

const applicationRetentionSettingsQuery = `SELECT alert_event_days, audit_event_days,
	cleanup_hour_utc, auto_cleanup_enabled, updated_at, running, last_started_at,
	last_completed_at, last_status, last_error_code, last_trigger, last_duration_ms,
	deleted_alert_events, deleted_audit_events, deleted_sessions
	FROM application_retention_settings WHERE id = 1`

// ApplicationRetentionStore 保存应用历史保留策略，并提供有界批量删除。
type ApplicationRetentionStore struct {
	db *sql.DB
}

func NewApplicationRetentionStore(db *sql.DB) *ApplicationRetentionStore {
	return &ApplicationRetentionStore{db: db}
}

func (store *ApplicationRetentionStore) Get(
	ctx context.Context,
) (model.ApplicationRetentionSettings, model.ApplicationRetentionStatus, error) {
	var settings model.ApplicationRetentionSettings
	var status model.ApplicationRetentionStatus
	var automatic, running int
	err := store.db.QueryRowContext(ctx, applicationRetentionSettingsQuery).Scan(
		&settings.AlertEventDays, &settings.AuditEventDays, &settings.CleanupHourUTC, &automatic,
		&settings.UpdatedAt, &running, &status.LastStartedAt, &status.LastCompletedAt,
		&status.LastStatus, &status.LastErrorCode, &status.LastTrigger, &status.LastDurationMS,
		&status.DeletedAlerts, &status.DeletedAudits, &status.DeletedSessions,
	)
	if err != nil {
		return settings, status, fmt.Errorf("get application retention settings: %w", err)
	}
	settings.AutoCleanupEnabled = automatic == 1
	status.Running = running == 1
	return settings, status, nil
}

func (store *ApplicationRetentionStore) Update(
	ctx context.Context,
	settings model.ApplicationRetentionSettings,
) (model.ApplicationRetentionSettings, error) {
	if err := settings.Validate(); err != nil {
		return model.ApplicationRetentionSettings{}, err
	}
	settings.UpdatedAt = model.NowUTC()
	_, err := store.db.ExecContext(ctx, `UPDATE application_retention_settings SET
		alert_event_days = ?, audit_event_days = ?, cleanup_hour_utc = ?,
		auto_cleanup_enabled = ?, updated_at = ? WHERE id = 1`,
		settings.AlertEventDays, settings.AuditEventDays, settings.CleanupHourUTC,
		settings.AutoCleanupEnabled, settings.UpdatedAt)
	if err != nil {
		return model.ApplicationRetentionSettings{}, fmt.Errorf("update application retention settings: %w", err)
	}
	return settings, nil
}

func (store *ApplicationRetentionStore) MarkStarted(
	ctx context.Context,
	trigger string,
	startedAt time.Time,
) error {
	_, err := store.db.ExecContext(ctx, `UPDATE application_retention_settings SET running = 1,
		last_started_at = ?, last_completed_at = NULL, last_status = ?, last_error_code = '',
		last_trigger = ?, last_duration_ms = 0, deleted_alert_events = 0,
		deleted_audit_events = 0, deleted_sessions = 0 WHERE id = 1`,
		startedAt, model.ApplicationRetentionStatusRunning, trigger)
	if err != nil {
		return fmt.Errorf("mark application retention started: %w", err)
	}
	return nil
}

func (store *ApplicationRetentionStore) MarkCompleted(
	ctx context.Context,
	completedAt time.Time,
	duration time.Duration,
	result string,
	errorCode string,
	counts model.ApplicationRetentionCounts,
) error {
	_, err := store.db.ExecContext(ctx, `UPDATE application_retention_settings SET running = 0,
		last_completed_at = ?, last_status = ?, last_error_code = ?, last_duration_ms = ?,
		deleted_alert_events = ?, deleted_audit_events = ?, deleted_sessions = ? WHERE id = 1`,
		completedAt, result, errorCode, duration.Milliseconds(),
		counts.AlertEvents, counts.AuditEvents, counts.Sessions)
	if err != nil {
		return fmt.Errorf("mark application retention completed: %w", err)
	}
	return nil
}

// RecoverInterrupted 把重启遗留的 running 状态结算为失败，避免清理结果被伪装成成功。
func (store *ApplicationRetentionStore) RecoverInterrupted(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `UPDATE application_retention_settings SET running = 0,
		last_completed_at = CURRENT_TIMESTAMP, last_status = ?, last_error_code = ?
		WHERE id = 1 AND running = 1`, model.ApplicationRetentionStatusFailed,
		interruptedApplicationRetentionCode)
	if err != nil {
		return fmt.Errorf("recover interrupted application retention: %w", err)
	}
	return nil
}

// DeleteResolvedAlertEventsBefore 只删除已解决的事件；活动事件永不按时间清理。
func (store *ApplicationRetentionStore) DeleteResolvedAlertEventsBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	return store.deleteBatch(ctx, `DELETE FROM alert_events WHERE id IN (
		SELECT id FROM alert_events WHERE status = ? AND resolved_at IS NOT NULL AND resolved_at < ?
		ORDER BY triggered_at ASC, id ASC LIMIT ?)`,
		"delete resolved alert events", model.AlertStatusResolved, cutoff, limit)
}

func (store *ApplicationRetentionStore) DeleteAuditEventsBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	return store.deleteBatch(ctx, `DELETE FROM admin_audit_events WHERE id IN (
		SELECT id FROM admin_audit_events WHERE created_at < ?
		ORDER BY created_at ASC, id ASC LIMIT ?)`,
		"delete administrator audit events", cutoff, limit)
}

// DeleteRevokedSessionsBefore 删除已过期或已吊销超过安全窗口的会话，保留活动会话。
func (store *ApplicationRetentionStore) DeleteRevokedSessionsBefore(
	ctx context.Context,
	now time.Time,
	revokedCutoff time.Time,
	limit int,
) (int64, error) {
	return store.deleteBatch(ctx, `DELETE FROM admin_sessions WHERE id IN (
		SELECT id FROM admin_sessions WHERE absolute_expires_at < ?
			OR (revoked_at IS NOT NULL AND revoked_at < ?)
		ORDER BY created_at ASC, id ASC LIMIT ?)`,
		"delete expired administrator sessions", now, revokedCutoff, limit)
}

func (store *ApplicationRetentionStore) deleteBatch(
	ctx context.Context,
	query string,
	label string,
	args ...any,
) (int64, error) {
	result, err := store.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%s rows affected: %w", label, err)
	}
	return deleted, nil
}
