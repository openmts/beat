package notificationqueue

import (
	"context"
	"encoding/json"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

type PreviewWrite struct {
	ChannelID        string            `json:"channel_id"`
	ExpectedRevision int64             `json:"expected_revision"`
	ChannelType      string            `json:"channel_type"`
	Config           json.RawMessage   `json:"config"`
	ClearSecrets     []string          `json:"clear_secrets"`
	TemplateSubject  string            `json:"template_subject"`
	TemplateText     string            `json:"template_text"`
	Subject          string            `json:"subject"`
	Text             string            `json:"text"`
	Data             map[string]string `json:"data"`
}

func (service *Service) Preview(ctx context.Context, actor Actor, input PreviewWrite) (notification.Preview, error) {
	if err := validateRequest(Request{SourceKey: "preview", SourceKind: "preview", SourceID: "preview", EventKind: "notification.test", Subject: input.Subject, Text: input.Text, Data: input.Data}); err != nil {
		return notification.Preview{}, err
	}
	var preview notification.Preview
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: actor, owner: true, recent: true}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		if err := service.consumeBudget(ctx, tx, budgetRule{kind: "preview", scope: actor.UserID, seconds: 60, limit: 30}); err != nil {
			return err
		}
		change := channelChange{actor: actor, id: input.ChannelID, write: model.NotificationChannelWrite{ChannelType: input.ChannelType, ExpectedRevision: input.ExpectedRevision}}
		_, _, previous, err := service.channelWriteState(ctx, tx, channelStateInput{change: change, access: access})
		if err != nil {
			return err
		}
		defer clear(previous)
		canonical, err := notification.CanonicalizeProvider(notification.ConfigWrite{ChannelType: input.ChannelType, Config: input.Config, Previous: previous, ClearSecrets: input.ClearSecrets, Enabled: false})
		if err != nil {
			return err
		}
		defer clear(canonical.Secret)
		payload := model.NotificationPayload{Subject: input.Subject, Text: input.Text, Data: input.Data}
		if payload.Subject == "" {
			payload.Subject = "Beat notification preview"
		}
		if payload.Text == "" {
			payload.Text = "Example notification message"
		}
		preview, err = notification.PreviewDispatch(model.NotificationDispatch{DeliveryID: "preview", ChannelType: input.ChannelType, Attempt: 1, Config: canonical.Secret, TemplateSubject: input.TemplateSubject, TemplateText: input.TemplateText, Payload: payload})
		if err != nil {
			return err
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.channel.preview", resourceType: "notification_channel", resourceID: input.ChannelID, detail: map[string]any{"channel_type": input.ChannelType}}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	return preview, err
}
