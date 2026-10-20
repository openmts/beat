package notificationqueue

import (
	"context"
	"database/sql"
	"time"

	"github.com/beat/backend/internal/model"
)

func (service *Service) Prune(ctx context.Context) error {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrQueueUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return err
	}
	now := service.now()
	successCutoff := now.Add(-time.Duration(settings.SuccessRetentionDays) * 24 * time.Hour).UnixMilli()
	deadCutoff := now.Add(-time.Duration(settings.DeadRetentionDays) * 24 * time.Hour).UnixMilli()
	if err := pruneDeliveryChains(ctx, tx, retentionCutoffs{success: successCutoff, dead: deadCutoff}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_messages WHERE id IN (
		SELECT m.id FROM notification_messages m WHERE NOT EXISTS(SELECT 1 FROM notification_deliveries d WHERE d.message_id=m.id)
		AND NOT EXISTS(SELECT 1 FROM notification_deliveries source WHERE source.state='dead'
		AND m.source_key='notification-failure:'||source.channel_id||':'||source.error_code||':'||CAST(source.completed_at/? AS TEXT))
		AND (m.routing_state='routed' OR m.retain_until<?) ORDER BY m.created_at LIMIT 128)`, failureWindowSeconds*1000, now.UnixMilli()); err != nil {
		return ErrQueueUnavailable
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_channel_revisions WHERE id IN (
		SELECT r.id FROM alert_channel_revisions r JOIN alert_channels c ON c.id=r.channel_id
		WHERE r.revision<>c.current_revision AND NOT EXISTS(SELECT 1 FROM notification_deliveries d WHERE d.channel_id=r.channel_id AND d.channel_revision=r.revision) LIMIT 128)`); err != nil {
		return ErrQueueUnavailable
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM notification_request_budgets WHERE window_start<?", now.Add(-time.Hour).Unix()); err != nil {
		return ErrQueueUnavailable
	}
	if err := tx.Commit(); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}

type QueueSnapshot struct {
	Counts                  model.NotificationDeliveryCounts
	OldestPendingAgeSeconds float64
	OpenCircuits            int64
	SQLiteBytes             int64
	SQLiteReusableBytes     int64
	FilesystemFreeBytes     *uint64
}

func (service *Service) Snapshot(ctx context.Context) (QueueSnapshot, error) {
	var snapshot QueueSnapshot
	rows, err := service.db.QueryContext(ctx, "SELECT state,COUNT(*) FROM notification_deliveries GROUP BY state")
	if err != nil {
		return snapshot, ErrQueueUnavailable
	}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			_ = rows.Close()
			return snapshot, ErrQueueUnavailable
		}
		setQueueCount(&snapshot.Counts, state, count)
	}
	if err := rows.Close(); err != nil {
		return snapshot, ErrQueueUnavailable
	}
	if err := rows.Err(); err != nil {
		return snapshot, ErrQueueUnavailable
	}
	var oldest sql.NullInt64
	if err := service.db.QueryRowContext(ctx, "SELECT MIN(created_at) FROM notification_deliveries WHERE state='pending'").Scan(&oldest); err != nil {
		return snapshot, ErrQueueUnavailable
	}
	if oldest.Valid {
		snapshot.OldestPendingAgeSeconds = max(0, service.now().Sub(time.UnixMilli(oldest.Int64)).Seconds())
	}
	if err := service.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_channel_health WHERE circuit='open' AND open_until>?", service.now().UnixMilli()).Scan(&snapshot.OpenCircuits); err != nil {
		return snapshot, ErrQueueUnavailable
	}
	if err := service.snapshotStorage(ctx, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func setQueueCount(counts *model.NotificationDeliveryCounts, state string, count int) {
	switch state {
	case "pending":
		counts.Pending = count
	case "leased":
		counts.Leased = count
	case "succeeded":
		counts.Succeeded = count
	case "dead":
		counts.Dead = count
	case "cancelled":
		counts.Cancelled = count
	}
}
