package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type ChannelEnabled struct {
	ID      string
	Enabled bool
}

func (service *Service) SetEnabled(ctx context.Context, actor Actor, change ChannelEnabled) (model.NotificationChannel, error) {
	err := service.changeChannelState(ctx, channelOperation{actor: actor, id: change.ID, enabled: change.Enabled})
	if err != nil {
		return model.NotificationChannel{}, err
	}
	if !change.Enabled {
		service.cancelChannel(change.ID)
	}
	return service.GetChannel(ctx, change.ID)
}

func (service *Service) DeleteChannel(ctx context.Context, actor Actor, id string) error {
	if err := service.changeChannelState(ctx, channelOperation{actor: actor, id: id, deleted: true}); err != nil {
		return err
	}
	service.cancelChannel(id)
	return nil
}

type channelOperation struct {
	actor            Actor
	id               string
	enabled, deleted bool
}

func (service *Service) changeChannelState(ctx context.Context, input channelOperation) error {
	return service.secrets.WithWrite(ctx, func(_ secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: input.actor, owner: input.deleted, recent: input.deleted}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM alert_channels WHERE id=? AND deleted_at IS NULL", input.id).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return ErrQueueUnavailable
		}
		action := "notification.channel.enable"
		var result sql.Result
		if input.deleted {
			action = "notification.channel.delete"
			result, err = tx.ExecContext(ctx, "UPDATE alert_channels SET enabled=0,deleted_at=?,updated_at=? WHERE id=?", service.now(), service.now(), id)
		} else {
			result, err = tx.ExecContext(ctx, "UPDATE alert_channels SET enabled=?,updated_at=? WHERE id=?", input.enabled, service.now(), id)
		}
		if err := exactlyOne(result, err); err != nil {
			return ErrQueueUnavailable
		}
		if input.deleted {
			if err := service.cancelChannelDeliveries(ctx, tx, id); err != nil {
				return err
			}
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: action, resourceType: "notification_channel", resourceID: id, detail: map[string]any{"enabled": input.enabled, "deleted": input.deleted}}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
}

func (service *Service) cancelChannelDeliveries(ctx context.Context, tx *sql.Tx, id string) error {
	now := service.now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET outcome='cancelled',completed_at=?,duration_ms=MAX(0,?-started_at),error_code='channel_deleted',error_stage='policy'
		WHERE outcome='in_progress' AND delivery_id IN(SELECT id FROM notification_deliveries WHERE channel_id=? AND state='leased')`, now, now, id); err != nil {
		return ErrQueueUnavailable
	}
	_, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state='cancelled',lease_token_hash=NULL,lease_expires_at=NULL,worker_id='',completed_at=?,updated_at=?,error_code='channel_deleted',error_stage='policy'
		WHERE channel_id=? AND state IN ('pending','leased')`, now, now, id)
	return err
}
