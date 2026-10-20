package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

const failureWindowSeconds = 15 * 60

type failureInput struct {
	completion completion
	access     secretbox.ResourceAccess
	observedAt time.Time
}

func (service *Service) failureMessage(ctx context.Context, tx *sql.Tx, input failureInput) error {
	lease, result := input.completion.lease, input.completion.result
	if input.observedAt.IsZero() {
		input.observedAt = service.now()
	}
	key := fmt.Sprintf("notification-failure:%s:%s:%d", lease.ChannelID, result.ErrorCode, input.observedAt.Unix()/failureWindowSeconds)
	if _, err := sourceReceipt(ctx, tx, key); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	request := Request{SourceKey: key, SourceKind: "notification_delivery", SourceID: lease.ChannelID, EventKind: "notification.delivery_failed",
		Subject: "[Beat] Notification delivery failed", Text: "A notification channel could not deliver a message. Review its delivery history.", UseSubscriptions: true}
	_, err := service.enqueueTx(ctx, tx, enqueueInput{request: request, access: input.access, excludeChannel: lease.ChannelID})
	return err
}

func (service *Service) deferCapacityFailure(ctx context.Context, tx *sql.Tx, input failureInput) error {
	err := service.failureMessage(ctx, tx, input)
	if errors.Is(err, ErrQueueCapacity) {
		// dead 行是恢复来源；队列满时仍提交终态，维护器腾出容量后补建失败通知。
		slog.Warn("notification failure event awaits queue capacity", "delivery_id", input.completion.lease.ID)
		return nil
	}
	return err
}

func (service *Service) ReconcileFailures(ctx context.Context) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		missing, err := missingFailureMessages(ctx, tx)
		if err != nil {
			return err
		}
		for _, input := range missing {
			input.access = access
			err := service.failureMessage(ctx, tx, input)
			if errors.Is(err, ErrQueueCapacity) {
				break
			}
			if err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
}

func missingFailureMessages(ctx context.Context, tx *sql.Tx) (missing []failureInput, resultErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.channel_id,d.error_code,d.completed_at FROM notification_deliveries d
		JOIN notification_messages m ON m.id=d.message_id WHERE d.state='dead' AND m.event_kind<>'notification.delivery_failed'
		AND d.completed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM notification_messages sent
		WHERE sent.source_key='notification-failure:'||d.channel_id||':'||d.error_code||':'||CAST(d.completed_at/? AS TEXT))
		ORDER BY d.completed_at,d.id LIMIT 64`, failureWindowSeconds*1000)
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var input failureInput
		var completed int64
		if err := rows.Scan(&input.completion.lease.ID, &input.completion.lease.ChannelID, &input.completion.result.ErrorCode, &completed); err != nil {
			return nil, ErrQueueUnavailable
		}
		input.observedAt = time.UnixMilli(completed)
		missing = append(missing, input)
	}
	return missing, rows.Err()
}
