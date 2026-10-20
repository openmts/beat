package notificationqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"math/big"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type completion struct {
	lease  deliveryLease
	result model.NotificationSendResult
}

func (service *Service) complete(ctx context.Context, input completion) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		state, err := service.completeTx(ctx, tx, input)
		if err != nil {
			return err
		}
		if state == "dead" && input.lease.EventKind != "notification.delivery_failed" {
			if err := service.deferCapacityFailure(ctx, tx, failureInput{completion: input, access: access}); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
}

func (service *Service) completeTx(ctx context.Context, tx *sql.Tx, input completion) (string, error) {
	lease, result := input.lease, input.result
	now := service.now()
	state, outcome := "dead", "permanent"
	next := now
	if result.Success {
		state, outcome = "succeeded", "succeeded"
	} else if result.Retryable {
		outcome = "retryable"
		if lease.Attempt < 6 {
			delay, err := service.retryDelay(lease.Attempt, result.RetryAfter)
			if err != nil {
				return "", err
			}
			state, next = "pending", now.Add(delay)
		}
	}
	var completed any
	if state != "pending" {
		completed = now.UnixMilli()
	}
	update, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state=?,next_attempt_at=?,completed_at=?,updated_at=?,lease_token_hash=NULL,lease_expires_at=NULL,worker_id='',error_code=?,error_stage=?,status_class=?
		WHERE id=? AND state='leased' AND lease_token_hash=? AND lease_expires_at>?`, state, next.UnixMilli(), completed, now.UnixMilli(), result.ErrorCode, result.Stage, result.StatusClass, lease.ID, lease.Hash, now.UnixMilli())
	if err != nil {
		return "", ErrQueueUnavailable
	}
	count, err := update.RowsAffected()
	if err != nil {
		return "", ErrQueueUnavailable
	}
	if count != 1 {
		return "", ErrLeaseLost
	}
	update, err = tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET completed_at=?,duration_ms=MAX(0,?-started_at),outcome=?,error_code=?,error_stage=?,status_class=?
		WHERE delivery_id=? AND attempt_number=? AND outcome='in_progress'`, now.UnixMilli(), now.UnixMilli(), outcome, result.ErrorCode, result.Stage, result.StatusClass, lease.ID, lease.Attempt)
	if err := exactlyOne(update, err); err != nil {
		return "", ErrQueueUnavailable
	}
	if err := service.updateHealth(ctx, tx, input); err != nil {
		return "", err
	}
	return state, nil
}

func (service *Service) retryDelay(attempt int, retryAfter time.Duration) (time.Duration, error) {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}
	if attempt < 1 || attempt > len(delays) {
		return 0, ErrInvalidRequest
	}
	delay := delays[attempt-1]
	if retryAfter > delay {
		delay = min(retryAfter, time.Hour)
	}
	jitter, err := rand.Int(service.random, big.NewInt(int64(delay/10)+1))
	if err != nil {
		return 0, ErrQueueUnavailable
	}
	return delay + time.Duration(jitter.Int64()), nil
}

func (service *Service) updateHealth(ctx context.Context, tx *sql.Tx, input completion) error {
	now := service.now().UnixMilli()
	lease, result := input.lease, input.result
	if result.Success {
		_, err := tx.ExecContext(ctx, `UPDATE notification_channel_health SET consecutive_failures=0,circuit='closed',open_until=NULL,last_success=?,error_code='',error_stage='',updated_at=? WHERE channel_id=? AND current_revision=?`, now, now, lease.ChannelID, lease.Revision)
		return err
	}
	if result.Retryable {
		_, err := tx.ExecContext(ctx, `UPDATE notification_channel_health SET consecutive_failures=consecutive_failures+1,
			circuit=CASE WHEN consecutive_failures+1>=5 THEN 'open' ELSE circuit END,
			open_until=CASE WHEN consecutive_failures+1>=5 THEN ? ELSE open_until END,last_failure=?,error_code=?,error_stage=?,updated_at=?
			WHERE channel_id=? AND current_revision=?`, service.now().Add(5*time.Minute).UnixMilli(), now, result.ErrorCode, result.Stage, now, lease.ChannelID, lease.Revision)
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE notification_channel_health SET last_failure=?,error_code=?,error_stage=?,updated_at=? WHERE channel_id=? AND current_revision=?", now, result.ErrorCode, result.Stage, now, lease.ChannelID, lease.Revision)
	return err
}
