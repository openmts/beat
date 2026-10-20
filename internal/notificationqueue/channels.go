package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

type channelChange struct {
	actor Actor
	id    string
	write model.NotificationChannelWrite
}

func (service *Service) CreateChannel(ctx context.Context, actor Actor, write model.NotificationChannelWrite) (model.NotificationChannel, error) {
	return service.writeChannel(ctx, channelChange{actor: actor, write: write})
}

func (service *Service) UpdateChannel(ctx context.Context, actor Actor, change ChannelUpdate) (model.NotificationChannel, error) {
	return service.writeChannel(ctx, channelChange{actor: actor, id: change.ID, write: change.Write})
}

type ChannelUpdate struct {
	ID    string
	Write model.NotificationChannelWrite
}

func (service *Service) writeChannel(ctx context.Context, change channelChange) (model.NotificationChannel, error) {
	if err := validateChannelWrite(change); err != nil {
		return model.NotificationChannel{}, err
	}
	prepared, err := service.prepareChannel(ctx, change)
	if err != nil {
		return model.NotificationChannel{}, err
	}
	defer clear(prepared.canonical.Secret)
	var result model.NotificationChannel
	err = service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: change.actor, owner: true, recent: true}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		if err := revalidatePreparedChannel(ctx, tx, prepared); err != nil {
			return err
		}
		if err := service.persistChannel(ctx, tx, channelPersistence{change: change, id: prepared.id, revision: prepared.revision, canonical: prepared.canonical, access: access}); err != nil {
			return err
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.channel.write", resourceType: "notification_channel", resourceID: prepared.id, detail: map[string]any{"revision": prepared.revision, "channel_type": change.write.ChannelType}}); err != nil {
			return err
		}
		result, err = channelView(ctx, tx, prepared.id)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	if err == nil {
		if !change.write.Enabled {
			service.cancelChannel(prepared.id)
		}
		service.signal()
	}
	return result, err
}

func validateChannelWrite(change channelChange) error {
	write := change.write
	if write.Name == "" || len(write.Name) > 200 || !cleanScalar(write.Name) {
		return ErrInvalidRequest
	}
	if change.id != "" && write.ExpectedRevision < 1 {
		return ErrConflict
	}
	if len(write.EventKinds) > len(enabledEventKinds) {
		return ErrInvalidRequest
	}
	for _, kind := range write.EventKinds {
		if !enabledEventKinds[kind] || kind == "notification.test" {
			return ErrInvalidRequest
		}
	}
	if _, err := notification.CompileTemplate(write.TemplateSubject, false); err != nil {
		return err
	}
	if _, err := notification.CompileTemplate(write.TemplateText, false); err != nil {
		return err
	}
	return nil
}

type channelStateInput struct {
	change channelChange
	access secretbox.ResourceAccess
}

func (service *Service) channelWriteState(ctx context.Context, database queryStore, input channelStateInput) (string, int64, []byte, error) {
	if input.change.id == "" {
		id, err := service.newID()
		return id, 1, nil, err
	}
	var revision int64
	var kind string
	var encrypted []byte
	err := database.QueryRowContext(ctx, `SELECT c.current_revision,r.channel_type,r.secret_ciphertext FROM alert_channels c JOIN alert_channel_revisions r
		ON r.channel_id=c.id AND r.revision=c.current_revision WHERE c.id=? AND c.deleted_at IS NULL`, input.change.id).Scan(&revision, &kind, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil, ErrNotFound
	}
	if err != nil {
		return "", 0, nil, ErrQueueUnavailable
	}
	if revision != input.change.write.ExpectedRevision {
		return "", 0, nil, ErrConflict
	}
	if kind != input.change.write.ChannelType {
		return input.change.id, revision + 1, nil, nil
	}
	previous, err := input.access.Open(encrypted, secretbox.NotificationChannelAAD(input.change.id, revision, kind))
	return input.change.id, revision + 1, previous, err
}

type channelPersistence struct {
	change    channelChange
	id        string
	revision  int64
	canonical notification.CanonicalConfig
	access    secretbox.ResourceAccess
}

func (service *Service) persistChannel(ctx context.Context, tx *sql.Tx, input channelPersistence) error {
	write := input.change.write
	kinds := append([]string{}, write.EventKinds...)
	sort.Strings(kinds)
	kinds = uniqueStrings(kinds)
	encoded, err := json.Marshal(kinds)
	if err != nil {
		return err
	}
	now := service.now()
	var result sql.Result
	if input.change.id == "" {
		result, err = tx.ExecContext(ctx, `INSERT INTO alert_channels(id,name,channel_type,enabled,current_revision,event_kinds_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, input.id, write.Name, write.ChannelType, write.Enabled, input.revision, string(encoded), now, now)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE alert_channels SET name=?,channel_type=?,enabled=?,current_revision=?,event_kinds_json=?,updated_at=? WHERE id=? AND current_revision=? AND deleted_at IS NULL`, write.Name, write.ChannelType, write.Enabled, input.revision, string(encoded), now, input.id, write.ExpectedRevision)
	}
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	if err := service.insertRevision(ctx, tx, input); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO notification_channel_health(channel_id,current_revision,updated_at) VALUES(?,?,?)
		ON CONFLICT(channel_id) DO UPDATE SET current_revision=excluded.current_revision,consecutive_failures=0,circuit='closed',open_until=NULL,error_code='',error_stage='',updated_at=excluded.updated_at`, input.id, input.revision, now.UnixMilli())
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func (service *Service) insertRevision(ctx context.Context, tx *sql.Tx, input channelPersistence) error {
	id, err := service.newID()
	if err != nil {
		return err
	}
	write := input.change.write
	sealed, err := input.access.Seal(input.canonical.Secret, secretbox.NotificationChannelAAD(input.id, input.revision, write.ChannelType))
	if err != nil {
		return err
	}
	present, err := json.Marshal(input.canonical.Present)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO alert_channel_revisions(id,channel_id,revision,channel_type,public_config_json,secret_ciphertext,secret_present_json,template_subject,template_text,created_by,created_at,change_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'owner_update')`, id, input.id, input.revision, write.ChannelType, string(input.canonical.Public), sealed, string(present), write.TemplateSubject, write.TemplateText, input.change.actor.UserID, service.now().UnixMilli())
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}
