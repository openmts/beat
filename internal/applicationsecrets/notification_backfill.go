package applicationsecrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

const migratedEventKinds = `["alert.acknowledged","alert.resolved","alert.triggered","availability.offline","availability.recovered","notification.delivery_failed","traffic.quota","traffic.report"]`

func (service *Service) initializeNotifications(ctx context.Context) error {
	present, err := hasNotificationSchema(ctx, service.db)
	if err != nil || !present {
		return err
	}
	var phase string
	if err := service.db.QueryRowContext(ctx, "SELECT phase FROM notification_secret_state WHERE id=1").Scan(&phase); err != nil {
		return err
	}
	if phase == "pending" {
		if err := service.convertNotificationChannels(ctx); err != nil {
			return err
		}
		phase = "sanitize"
	}
	if phase == "sanitize" {
		if _, err := service.db.ExecContext(ctx, "VACUUM"); err != nil {
			return err
		}
		if err := checkpointSQLite(ctx, service.db); err != nil {
			return err
		}
		if err := service.checkpoint("notification_sanitized"); err != nil {
			return err
		}
		result, err := service.db.ExecContext(ctx, "UPDATE notification_secret_state SET phase='ready',updated_at=? WHERE id=1 AND phase='sanitize'", service.now().UnixMilli())
		if err := requireOne(result, err); err != nil {
			return err
		}
	}
	return validateNotificationState(ctx, service.db)
}

func (service *Service) convertNotificationChannels(ctx context.Context) error {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "PRAGMA secure_delete=ON"); err != nil {
		return err
	}
	cursor := ""
	for {
		channels, err := legacyChannelBatch(ctx, tx, cursor)
		if err != nil {
			return err
		}
		if len(channels) == 0 {
			break
		}
		for _, channel := range channels {
			if err := service.convertNotificationChannel(ctx, tx, channel); err != nil {
				return err
			}
			cursor = channel.id
		}
	}
	if err := service.checkpoint("notification_before_commit"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE alert_channels DROP COLUMN config"); err != nil {
		return err
	}
	if _, err := validateResources(ctx, tx, validationConfig{ring: service.ring, requireActive: true}); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE notification_secret_state SET phase='sanitize',updated_at=? WHERE id=1 AND phase='pending'", service.now().UnixMilli())
	if err := requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

type legacyNotificationChannel struct {
	id, kind, config string
	enabled          bool
}

func legacyChannelBatch(ctx context.Context, tx *sql.Tx, cursor string) (channels []legacyNotificationChannel, resultErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,channel_type,COALESCE(config,''),enabled FROM alert_channels
		WHERE id>? OR ?='' ORDER BY id LIMIT ?`, cursor, cursor, batchSize)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var channel legacyNotificationChannel
		if err := rows.Scan(&channel.id, &channel.kind, &channel.config, &channel.enabled); err != nil {
			return nil, err
		}
		if channel.id == "" || len(channel.config) > notification.MaximumConfigBytes {
			return nil, ErrUnhealthy
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

func (service *Service) convertNotificationChannel(ctx context.Context, tx *sql.Tx, channel legacyNotificationChannel) error {
	converted, err := notification.ConvertLegacyChannel(channel.kind, channel.config, channel.enabled)
	if err != nil {
		return err
	}
	defer clear(converted.Canonical.Secret)
	ciphertext, err := service.ring.Seal(converted.Canonical.Secret, secretbox.NotificationChannelAAD(channel.id, 1, channel.kind))
	if err != nil {
		return err
	}
	present, err := json.Marshal(converted.Canonical.Present)
	if err != nil {
		return err
	}
	id, err := uuid.NewRandomFromReader(service.random)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO alert_channel_revisions
		(id,channel_id,revision,channel_type,public_config_json,secret_ciphertext,secret_present_json,template_subject,template_text,created_by,created_at,change_reason)
		VALUES(?,?,1,?,?,?,?, '',?,'migration:v10',?,'legacy_migration')`, id.String(), channel.id, channel.kind, string(converted.Canonical.Public), ciphertext, string(present), converted.TemplateText, service.now().UnixMilli())
	if err := requireOne(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, "UPDATE alert_channels SET config='',current_revision=1,event_kinds_json=? WHERE id=? AND current_revision=0", migratedEventKinds, channel.id)
	if err := requireOne(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, "INSERT INTO notification_channel_health(channel_id,current_revision,updated_at) VALUES(?,1,?)", channel.id, service.now().UnixMilli())
	return requireOne(result, err)
}
