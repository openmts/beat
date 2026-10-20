package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) TestChannel(ctx context.Context, actor Actor, id string) (Receipt, error) {
	var receipt Receipt
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		rule := authorityRule{actor: actor}
		if _, err := service.authorize(ctx, tx, rule); err != nil {
			return err
		}
		key, err := service.testSourceKey(actor, id)
		if err != nil {
			return err
		}
		if previous, err := sourceReceipt(ctx, tx, key); err == nil {
			receipt = previous
			receipt.Duplicate = true
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		channel, err := currentChannel(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := service.testBudget(ctx, tx, testBudgetInput{actorID: actor.UserID, channelID: id}); err != nil {
			return err
		}
		request := Request{SourceKey: key, SourceKind: "notification_test", SourceID: id, EventKind: "notification.test", Subject: "[Beat] Test notification", Text: "This is a test notification from Beat.", Data: map[string]string{"event.occurred_at": service.now().Format(time.RFC3339)}}
		receipt, err = service.enqueueTx(ctx, tx, enqueueInput{request: request, access: access, forceChannel: &channel})
		if err != nil {
			return err
		}
		if err := service.audit(ctx, tx, auditInput{rule: rule, action: "notification.channel.test", resourceType: "notification_delivery", resourceID: receipt.Deliveries[0].ID, detail: map[string]any{"channel_id": id, "revision": channel.revision}}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	if err == nil {
		service.signal()
	}
	return receipt, err
}

func (service *Service) testSourceKey(actor Actor, id string) (string, error) {
	requestID := actor.RequestID
	if requestID == "" {
		var err error
		requestID, err = service.newID()
		if err != nil {
			return "", err
		}
	}
	return "notification-test:" + actor.UserID + ":" + id + ":" + requestID, nil
}

func currentChannel(ctx context.Context, tx *sql.Tx, id string) (selectedChannel, error) {
	channel := selectedChannel{id: id}
	err := tx.QueryRowContext(ctx, "SELECT current_revision FROM alert_channels WHERE id=? AND deleted_at IS NULL", id).Scan(&channel.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return channel, ErrNotFound
	}
	if err != nil || channel.revision < 1 {
		return channel, ErrQueueUnavailable
	}
	return channel, nil
}

type testBudgetInput struct{ actorID, channelID string }

func (service *Service) testBudget(ctx context.Context, tx *sql.Tx, input testBudgetInput) error {
	for _, budget := range []budgetRule{{kind: "test_channel", scope: input.channelID, seconds: 600, limit: 5}, {kind: "test_actor", scope: input.actorID, seconds: 3600, limit: 30}} {
		if err := service.consumeBudget(ctx, tx, budget); err != nil {
			return err
		}
	}
	return nil
}

type budgetRule struct {
	kind, scope string
	seconds     int64
	limit       int
}

func (service *Service) consumeBudget(ctx context.Context, tx *sql.Tx, rule budgetRule) error {
	window := service.now().Unix() / rule.seconds * rule.seconds
	result, err := tx.ExecContext(ctx, `INSERT INTO notification_request_budgets(kind,scope,window_start,request_count) VALUES(?,?,?,1)
		ON CONFLICT(kind,scope,window_start) DO UPDATE SET request_count=request_count+1 WHERE request_count<?`, rule.kind, rule.scope, window, rule.limit)
	if err != nil {
		return ErrQueueUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ErrQueueUnavailable
	}
	if count != 1 {
		return ErrRateLimited
	}
	return nil
}
