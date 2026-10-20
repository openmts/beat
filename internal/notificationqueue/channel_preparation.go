package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

type preparedChannel struct {
	id, previousID string
	revision       int64
	policyRevision int64
	canonical      notification.CanonicalConfig
}

func (service *Service) prepareChannel(ctx context.Context, change channelChange) (preparedChannel, error) {
	prepared := preparedChannel{previousID: change.id}
	var settings model.NotificationSettings
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		if _, err := service.authorize(ctx, service.db, authorityRule{actor: change.actor, owner: true, recent: true}); err != nil {
			return err
		}
		var previous []byte
		id, revision, previous, err := service.channelWriteState(ctx, service.db, channelStateInput{change: change, access: access})
		if err != nil {
			return err
		}
		defer clear(previous)
		prepared.id, prepared.revision = id, revision
		settings, err = readSettings(ctx, service.db)
		if err != nil {
			return err
		}
		prepared.policyRevision = settings.Revision
		prepared.canonical, err = notification.CanonicalizeProvider(notification.ConfigWrite{ChannelType: change.write.ChannelType, Config: change.write.Config, Previous: previous, ClearSecrets: change.write.ClearSecrets, Enabled: change.write.Enabled})
		return err
	})
	if err == nil {
		err = service.validateChannelEgress(ctx, settings.EgressPolicy, prepared.canonical.Secret)
	}
	if err != nil {
		clear(prepared.canonical.Secret)
		return preparedChannel{}, err
	}
	return prepared, nil
}

func revalidatePreparedChannel(ctx context.Context, tx *sql.Tx, prepared preparedChannel) error {
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return err
	}
	if settings.Revision != prepared.policyRevision {
		return ErrConflict
	}
	if prepared.previousID == "" {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM alert_channels WHERE deleted_at IS NULL").Scan(&count); err != nil {
			return ErrQueueUnavailable
		}
		if count >= MaximumChannels {
			return ErrQueueCapacity
		}
		return nil
	}
	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT current_revision FROM alert_channels WHERE id=? AND deleted_at IS NULL", prepared.id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return ErrQueueUnavailable
	}
	if revision != prepared.revision-1 {
		return ErrConflict
	}
	return nil
}
