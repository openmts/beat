package notificationqueue

import (
	"context"
	"encoding/json"
	"net"
	"strconv"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) Settings(ctx context.Context) (model.NotificationSettings, error) {
	return readSettings(ctx, service.db)
}

func readSettings(ctx context.Context, database queryStore) (model.NotificationSettings, error) {
	var settings model.NotificationSettings
	var policy string
	err := database.QueryRowContext(ctx, "SELECT revision,success_retention_days,dead_retention_days,maximum_workers,egress_policy_json FROM notification_settings WHERE id=1").Scan(
		&settings.Revision, &settings.SuccessRetentionDays, &settings.DeadRetentionDays, &settings.MaximumWorkers, &policy)
	if err != nil {
		return settings, ErrQueueUnavailable
	}
	if err := json.Unmarshal([]byte(policy), &settings.EgressPolicy); err != nil {
		return settings, ErrQueueUnavailable
	}
	return settings, nil
}

func (service *Service) UpdateSettings(ctx context.Context, actor Actor, write model.NotificationSettingsWrite) (model.NotificationSettings, error) {
	if write.ExpectedRevision < 1 || write.SuccessRetentionDays < 7 || write.SuccessRetentionDays > 365 || write.DeadRetentionDays < 30 || write.DeadRetentionDays > 730 || write.MaximumWorkers < 1 || write.MaximumWorkers > 32 {
		return model.NotificationSettings{}, ErrInvalidRequest
	}
	if err := notification.ValidateEgressPolicy(write.EgressPolicy, service.now()); err != nil {
		return model.NotificationSettings{}, err
	}
	var updated model.NotificationSettings
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
		encoded, err := service.encodeSettingsPolicy(actor, write)
		if err != nil {
			return ErrInvalidRequest
		}
		result, err := tx.ExecContext(ctx, `UPDATE notification_settings SET revision=revision+1,success_retention_days=?,dead_retention_days=?,maximum_workers=?,egress_policy_json=?,updated_at=? WHERE id=1 AND revision=?`,
			write.SuccessRetentionDays, write.DeadRetentionDays, write.MaximumWorkers, string(encoded), service.now().UnixMilli(), write.ExpectedRevision)
		if err != nil {
			return ErrQueueUnavailable
		}
		count, err := result.RowsAffected()
		if err != nil {
			return ErrQueueUnavailable
		}
		if count != 1 {
			return ErrConflict
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.settings.update", resourceType: "notification_settings", resourceID: "global", detail: map[string]any{"revision": write.ExpectedRevision + 1}}); err != nil {
			return err
		}
		updated, err = readSettings(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	return updated, err
}

func (service *Service) encodeSettingsPolicy(actor Actor, write model.NotificationSettingsWrite) ([]byte, error) {
	policy := write.EgressPolicy
	policy.Exceptions = append([]model.NotificationEgressException{}, write.EgressPolicy.Exceptions...)
	policy.Revision = write.ExpectedRevision + 1
	now := service.now()
	for index := range policy.Exceptions {
		policy.Exceptions[index].CreatedBy, policy.Exceptions[index].CreatedAt = actor.UserID, now
	}
	return json.Marshal(policy)
}

func (service *Service) validateChannelEgress(ctx context.Context, policy model.NotificationEgressPolicy, raw json.RawMessage) error {
	config, err := notification.DecodeProvider(raw)
	if err != nil {
		return err
	}
	if config.Endpoint != "" {
		return service.egress.Validate(ctx, config.Endpoint, policy)
	}
	if config.Host != "" {
		scheme := "https"
		if config.Security == "none" {
			scheme = "http"
		}
		return service.egress.Validate(ctx, scheme+"://"+net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), policy)
	}
	return nil
}
