package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

func (service *Service) GetChannel(ctx context.Context, id string) (model.NotificationChannel, error) {
	return channelView(ctx, service.db, id)
}

func (service *Service) ListChannels(ctx context.Context) ([]model.NotificationChannel, error) {
	rows, err := service.db.QueryContext(ctx, "SELECT id FROM alert_channels WHERE deleted_at IS NULL ORDER BY created_at,id")
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, ErrQueueUnavailable
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrQueueUnavailable
	}
	channels := make([]model.NotificationChannel, 0, len(ids))
	for _, id := range ids {
		channel, err := channelView(ctx, service.db, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, nil
}

func channelView(ctx context.Context, database queryStore, id string) (model.NotificationChannel, error) {
	var channel model.NotificationChannel
	var kinds, public, present string
	var open, success, failure sql.NullInt64
	err := database.QueryRowContext(ctx, `SELECT c.id,c.name,c.channel_type,c.enabled,c.current_revision,c.event_kinds_json,c.created_at,c.updated_at,
		r.public_config_json,r.secret_present_json,r.template_subject,r.template_text,
		COALESCE(h.consecutive_failures,0),COALESCE(h.circuit,'closed'),h.open_until,h.last_success,h.last_failure,
		COALESCE(h.error_code,''),COALESCE(h.error_stage,''),
		(SELECT COUNT(*) FROM notification_deliveries WHERE channel_id=c.id AND state IN ('pending','leased'))
		FROM alert_channels c JOIN alert_channel_revisions r ON r.channel_id=c.id AND r.revision=c.current_revision
		LEFT JOIN notification_channel_health h ON h.channel_id=c.id WHERE c.id=? AND c.deleted_at IS NULL`, id).Scan(
		&channel.ID, &channel.Name, &channel.ChannelType, &channel.Enabled, &channel.CurrentRevision, &kinds, &channel.CreatedAt, &channel.UpdatedAt,
		&public, &present, &channel.TemplateSubject, &channel.TemplateText, &channel.Health.ConsecutiveFailures, &channel.Health.Circuit, &open, &success, &failure,
		&channel.Health.ErrorCode, &channel.Health.ErrorStage, &channel.Health.QueueDepth)
	if errors.Is(err, sql.ErrNoRows) {
		return channel, ErrNotFound
	}
	if err != nil {
		return channel, ErrQueueUnavailable
	}
	if err := json.Unmarshal([]byte(kinds), &channel.EventKinds); err != nil {
		return channel, ErrQueueUnavailable
	}
	if err := json.Unmarshal([]byte(present), &channel.SecretPresent); err != nil || !json.Valid([]byte(public)) {
		return channel, ErrQueueUnavailable
	}
	channel.Config = json.RawMessage(public)
	channel.Health.Revision = channel.CurrentRevision
	channel.Health.OpenUntil, channel.Health.LastSuccess, channel.Health.LastFailure = nullableTime(open), nullableTime(success), nullableTime(failure)
	return channel, nil
}

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}
