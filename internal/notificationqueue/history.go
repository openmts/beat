package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

type DeliveryFilter struct {
	State, ChannelID string
	Offset, Limit    int
}

const deliveryColumns = `d.id,d.channel_id,d.channel_revision,d.state,d.attempt_count,d.message_id,c.name,r.channel_type,m.event_kind,m.source_kind,m.source_id,
	d.next_attempt_at,d.error_code,d.error_stage,d.status_class,COALESCE(d.retry_of,''),d.created_at,d.completed_at`

const deliveryJoins = ` FROM notification_deliveries d JOIN notification_messages m ON m.id=d.message_id JOIN alert_channels c ON c.id=d.channel_id
	JOIN alert_channel_revisions r ON r.channel_id=d.channel_id AND r.revision=d.channel_revision`

func (service *Service) ListDeliveries(ctx context.Context, filter DeliveryFilter) (model.NotificationDeliveryPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 || filter.Offset > 1000000 || filter.Limit < 1 || filter.Limit > 100 {
		return model.NotificationDeliveryPage{}, ErrInvalidRequest
	}
	if filter.State != "" && filter.State != "pending" && filter.State != "leased" && filter.State != "succeeded" && filter.State != "dead" && filter.State != "cancelled" {
		return model.NotificationDeliveryPage{}, ErrInvalidRequest
	}
	where, args := deliveryWhere(filter)
	page := model.NotificationDeliveryPage{Items: []model.NotificationDelivery{}, Offset: filter.Offset, Limit: filter.Limit}
	if err := service.db.QueryRowContext(ctx, "SELECT COUNT(*)"+deliveryJoins+where, args...).Scan(&page.Total); err != nil {
		return page, ErrQueueUnavailable
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := service.db.QueryContext(ctx, "SELECT "+deliveryColumns+deliveryJoins+where+" ORDER BY d.created_at DESC,d.id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return page, ErrQueueUnavailable
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		delivery, err := scanDelivery(rows.Scan)
		if err != nil {
			return page, ErrQueueUnavailable
		}
		page.Items = append(page.Items, delivery)
	}
	return page, rows.Err()
}

func deliveryWhere(filter DeliveryFilter) (string, []any) {
	where := " WHERE 1=1"
	args := []any{}
	if filter.State != "" {
		where += " AND d.state=?"
		args = append(args, filter.State)
	}
	if filter.ChannelID != "" {
		where += " AND d.channel_id=?"
		args = append(args, filter.ChannelID)
	}
	return where, args
}

func scanDelivery(scan func(...any) error) (model.NotificationDelivery, error) {
	var delivery model.NotificationDelivery
	var next, created int64
	var completed sql.NullInt64
	err := scan(&delivery.ID, &delivery.ChannelID, &delivery.ChannelRevision, &delivery.State, &delivery.AttemptCount, &delivery.MessageID, &delivery.ChannelName, &delivery.ChannelType,
		&delivery.EventKind, &delivery.SourceKind, &delivery.SourceID, &next, &delivery.ErrorCode, &delivery.ErrorStage, &delivery.StatusClass, &delivery.RetryOf, &created, &completed)
	delivery.NextAttemptAt = *nullableTime(sql.NullInt64{Valid: true, Int64: next})
	delivery.CreatedAt = *nullableTime(sql.NullInt64{Valid: true, Int64: created})
	delivery.CompletedAt = nullableTime(completed)
	return delivery, err
}

func (service *Service) GetDelivery(ctx context.Context, id string) (model.NotificationDelivery, error) {
	row := service.db.QueryRowContext(ctx, "SELECT "+deliveryColumns+deliveryJoins+" WHERE d.id=?", id)
	delivery, err := scanDelivery(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery, ErrNotFound
	}
	if err != nil {
		return delivery, ErrQueueUnavailable
	}
	delivery.Attempts, err = service.deliveryAttempts(ctx, id)
	return delivery, err
}

func (service *Service) deliveryAttempts(ctx context.Context, id string) ([]model.NotificationAttempt, error) {
	rows, err := service.db.QueryContext(ctx, "SELECT attempt_number,started_at,completed_at,duration_ms,outcome,error_code,error_stage,status_class FROM notification_delivery_attempts WHERE delivery_id=? ORDER BY attempt_number", id)
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { _ = rows.Close() }()
	attempts := []model.NotificationAttempt{}
	for rows.Next() {
		var attempt model.NotificationAttempt
		var started int64
		var completed sql.NullInt64
		if err := rows.Scan(&attempt.Number, &started, &completed, &attempt.DurationMS, &attempt.Outcome, &attempt.ErrorCode, &attempt.ErrorStage, &attempt.StatusClass); err != nil {
			return nil, ErrQueueUnavailable
		}
		attempt.StartedAt = *nullableTime(sql.NullInt64{Valid: true, Int64: started})
		attempt.CompletedAt = nullableTime(completed)
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}
