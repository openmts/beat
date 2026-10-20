package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type RetryRequest struct {
	ID               string
	ConfirmDuplicate bool
}

func (service *Service) RetryDelivery(ctx context.Context, actor Actor, request RetryRequest) (model.NotificationDeliveryRef, error) {
	if !request.ConfirmDuplicate {
		return model.NotificationDeliveryRef{}, ErrInvalidRequest
	}
	var result model.NotificationDeliveryRef
	err := service.secrets.WithWrite(ctx, func(_ secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: actor, owner: true, recent: true}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		source, err := retrySource(ctx, tx, request.ID)
		if err != nil {
			return err
		}
		if existing, err := retryChild(ctx, tx, request.ID); err == nil {
			result = existing
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		selected, err := currentChannel(ctx, tx, source.channel)
		if err != nil {
			return err
		}
		if err := checkQueueCapacity(ctx, tx, 1); err != nil {
			return err
		}
		id, err := service.insertDelivery(ctx, tx, newDelivery{messageID: source.message, channel: selected, retryOf: request.ID})
		if err != nil {
			return err
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.delivery.retry", resourceType: "notification_delivery", resourceID: id, detail: map[string]any{"retry_of": request.ID, "revision": selected.revision}}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		result = model.NotificationDeliveryRef{ID: id, ChannelID: source.channel, ChannelRevision: selected.revision, State: "pending"}
		return nil
	})
	if err == nil {
		service.signal()
	}
	return result, err
}

type retrySourceRecord struct{ message, channel string }

func retrySource(ctx context.Context, tx *sql.Tx, id string) (retrySourceRecord, error) {
	var source retrySourceRecord
	var state string
	err := tx.QueryRowContext(ctx, "SELECT message_id,channel_id,state FROM notification_deliveries WHERE id=?", id).Scan(&source.message, &source.channel, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrNotFound
	}
	if err != nil {
		return source, ErrQueueUnavailable
	}
	if state != "dead" {
		return source, ErrConflict
	}
	return source, nil
}

func retryChild(ctx context.Context, tx *sql.Tx, id string) (model.NotificationDeliveryRef, error) {
	var ref model.NotificationDeliveryRef
	err := tx.QueryRowContext(ctx, "SELECT id,channel_id,channel_revision,state,attempt_count FROM notification_deliveries WHERE retry_of=?", id).Scan(&ref.ID, &ref.ChannelID, &ref.ChannelRevision, &ref.State, &ref.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ref, ErrNotFound
	}
	if err != nil {
		return ref, ErrQueueUnavailable
	}
	return ref, nil
}

func (service *Service) CancelDelivery(ctx context.Context, actor Actor, id string) error {
	channel := ""
	err := service.secrets.WithWrite(ctx, func(_ secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: actor, owner: true, recent: true}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT channel_id,state FROM notification_deliveries WHERE id=?", id).Scan(&channel, &state); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return ErrQueueUnavailable
		}
		if state != "pending" && state != "leased" {
			return ErrConflict
		}
		now := service.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, "UPDATE notification_delivery_attempts SET outcome='cancelled',completed_at=?,duration_ms=MAX(0,?-started_at),error_code='operator_cancelled',error_stage='policy' WHERE delivery_id=? AND outcome='in_progress'", now, now, id); err != nil {
			return ErrQueueUnavailable
		}
		result, err := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='cancelled',lease_token_hash=NULL,lease_expires_at=NULL,worker_id='',completed_at=?,updated_at=?,error_code='operator_cancelled',error_stage='policy' WHERE id=? AND state IN ('pending','leased')", now, now, id)
		if err := exactlyOne(result, err); err != nil {
			return ErrConflict
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.delivery.cancel", resourceType: "notification_delivery", resourceID: id, detail: map[string]any{}}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	if err == nil {
		service.cancelDelivery(channel, id)
	}
	return err
}
