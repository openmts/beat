package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type activeDelivery struct {
	id     string
	cancel context.CancelFunc
}

var errChannelPaused = errors.New("notification channel is paused")

func (service *Service) maintain(ctx context.Context) error {
	if err := service.ReconcileFailures(ctx); err != nil {
		return err
	}
	return service.Prune(ctx)
}

func (service *Service) work(ctx context.Context, workerID string) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		lease, err := service.claim(ctx, workerID)
		service.reportFailure(ctx, err)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return ErrQueueUnavailable
		}
		if lease == nil {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
			case <-service.wake:
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		if err := service.process(ctx, *lease); err != nil && !errors.Is(err, ErrLeaseLost) {
			return err
		}
		service.signal()
	}
}

func (service *Service) process(ctx context.Context, lease deliveryLease) error {
	started := time.Now()
	requestCtx, cancel := context.WithCancel(ctx)
	service.mu.Lock()
	if old, exists := service.active[lease.ChannelID]; exists {
		old.cancel()
	}
	service.active[lease.ChannelID] = activeDelivery{id: lease.ID, cancel: cancel}
	service.mu.Unlock()
	defer func() {
		cancel()
		service.mu.Lock()
		if active, exists := service.active[lease.ChannelID]; exists && active.id == lease.ID {
			delete(service.active, lease.ChannelID)
		}
		service.mu.Unlock()
	}()
	dispatch, err := service.readDispatch(requestCtx, lease)
	result := model.NotificationSendResult{}
	if errors.Is(err, errChannelPaused) {
		result = model.NotificationSendResult{Retryable: true, ErrorCode: "channel_disabled", Stage: "policy"}
	} else if err != nil {
		if ctx.Err() != nil {
			result = model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled", Stage: "request"}
		} else {
			return err
		}
	} else {
		result = service.sendSafely(requestCtx, dispatch)
		clear(dispatch.Config)
	}
	finishCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := service.complete(finishCtx, completion{lease: lease, result: result}); err != nil {
		return err
	}
	service.recordAttempt(attemptObservation{lease: lease, result: result, duration: time.Since(started)})
	if result.Success {
		service.success.Add(1)
	} else {
		service.failed.Add(1)
	}
	slog.Info("notification delivery attempt completed", "delivery_id", lease.ID, "provider", lease.ChannelType, "attempt", lease.Attempt, "success", result.Success, "error_code", result.ErrorCode, "duration_ms", time.Since(started).Milliseconds())
	return nil
}

func (service *Service) sendSafely(ctx context.Context, dispatch model.NotificationDispatch) (result model.NotificationSendResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = model.NotificationSendResult{ErrorCode: "sender_failed", Stage: "dispatch"}
		}
	}()
	return service.sender.Send(ctx, dispatch)
}

func (service *Service) readDispatch(ctx context.Context, lease deliveryLease) (dispatch model.NotificationDispatch, resultErr error) {
	dispatch = model.NotificationDispatch{DeliveryID: lease.ID, ChannelID: lease.ChannelID, ChannelType: lease.ChannelType, Revision: lease.Revision, Attempt: lease.Attempt}
	defer func() {
		if resultErr != nil {
			clear(dispatch.Config)
			dispatch.Config, dispatch.Payload = nil, model.NotificationPayload{}
		}
	}()
	var config, payload []byte
	var enabled bool
	var deleted sql.NullTime
	err := service.db.QueryRowContext(ctx, `SELECT r.secret_ciphertext,r.template_subject,r.template_text,m.payload_encrypted,c.enabled,c.deleted_at
		FROM notification_deliveries d JOIN notification_messages m ON m.id=d.message_id JOIN alert_channels c ON c.id=d.channel_id
		JOIN alert_channel_revisions r ON r.channel_id=d.channel_id AND r.revision=d.channel_revision
		WHERE d.id=? AND d.state='leased' AND d.lease_token_hash=? AND d.lease_expires_at>?`, lease.ID, lease.Hash, service.now().UnixMilli()).Scan(
		&config, &dispatch.TemplateSubject, &dispatch.TemplateText, &payload, &enabled, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return dispatch, ErrLeaseLost
	}
	if err != nil {
		return dispatch, ErrQueueUnavailable
	}
	if deleted.Valid || !enabled && lease.EventKind != "notification.test" {
		return dispatch, errChannelPaused
	}
	dispatch.Config, err = service.secrets.OpenResource(ctx, config, secretbox.NotificationChannelAAD(lease.ChannelID, lease.Revision, lease.ChannelType))
	if err != nil {
		return dispatch, ErrQueueUnavailable
	}
	plain, err := service.secrets.OpenResource(ctx, payload, secretbox.NotificationMessageAAD(lease.MessageID))
	if err != nil {
		clear(dispatch.Config)
		return dispatch, ErrQueueUnavailable
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &dispatch.Payload); err != nil {
		clear(dispatch.Config)
		return dispatch, ErrQueueUnavailable
	}
	settings, err := readSettings(ctx, service.db)
	if err != nil {
		clear(dispatch.Config)
		return dispatch, err
	}
	dispatch.Policy = settings.EgressPolicy
	return dispatch, nil
}

func (service *Service) cancelChannel(id string) {
	service.mu.Lock()
	if active, exists := service.active[id]; exists {
		active.cancel()
	}
	service.mu.Unlock()
	service.signal()
}

func (service *Service) cancelDelivery(channelID, deliveryID string) {
	service.mu.Lock()
	if active, exists := service.active[channelID]; exists && active.id == deliveryID {
		active.cancel()
	}
	service.mu.Unlock()
	service.signal()
}
