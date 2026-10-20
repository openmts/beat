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

func (service *Service) Enqueue(ctx context.Context, request Request, mutation func(*sql.Tx) error) (Receipt, error) {
	if err := validateRequest(request); err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		existing, err := sourceReceipt(ctx, tx, request.SourceKey)
		if err == nil {
			receipt = existing
			receipt.Duplicate = true
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if mutation != nil {
			if err := mutation(tx); err != nil {
				return err
			}
		}
		receipt, err = service.enqueueTx(ctx, tx, enqueueInput{request: request, access: access})
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	if errors.Is(err, ErrSkipMutation) {
		return Receipt{Skipped: true, Deliveries: []model.NotificationDeliveryRef{}}, nil
	}
	service.recordEnqueueOutcome(receipt, err)
	if err != nil {
		return Receipt{}, err
	}
	service.signal()
	return receipt, nil
}

func (service *Service) recordEnqueueOutcome(receipt Receipt, err error) {
	if errors.Is(err, ErrQueueUnavailable) {
		service.persistenceBroken.Store(true)
		service.observe(err)
		slog.Error("notification outbox persistence failed", "error_code", "notification_queue_unavailable")
	}
	if err == nil && !receipt.Duplicate && !receipt.Skipped {
		service.persistenceBroken.Store(false)
	}
}

type enqueueInput struct {
	request        Request
	access         secretbox.ResourceAccess
	excludeChannel string
	forceChannel   *selectedChannel
}

func (service *Service) enqueueTx(ctx context.Context, tx *sql.Tx, input enqueueInput) (Receipt, error) {
	channels, err := selectChannels(ctx, tx, input)
	if err != nil {
		return Receipt{}, err
	}
	if err := checkQueueCapacity(ctx, tx, len(channels)); err != nil {
		return Receipt{}, err
	}
	id, err := service.newID()
	if err != nil {
		return Receipt{}, err
	}
	sealed, err := encryptMessage(input, id)
	if err != nil {
		return Receipt{}, err
	}
	routing := "not_routed"
	if len(channels) > 0 {
		routing = "routed"
	}
	var retention int
	if err := tx.QueryRowContext(ctx, "SELECT dead_retention_days FROM notification_settings WHERE id=1").Scan(&retention); err != nil {
		return Receipt{}, ErrQueueUnavailable
	}
	now := service.now()
	result, err := tx.ExecContext(ctx, `INSERT INTO notification_messages
		(id,source_key,source_kind,source_id,event_kind,payload_encrypted,payload_version,routing_state,selected_channels,created_at,retain_until)
		VALUES(?,?,?,?,?,?,1,?,?,?,?)`, id, input.request.SourceKey, input.request.SourceKind, input.request.SourceID, input.request.EventKind,
		sealed, routing, len(channels), now.UnixMilli(), now.Add(time.Duration(retention)*24*time.Hour).UnixMilli())
	if err := exactlyOne(result, err); err != nil {
		return Receipt{}, ErrQueueUnavailable
	}
	for _, channel := range channels {
		if _, err := service.insertDelivery(ctx, tx, newDelivery{messageID: id, channel: channel}); err != nil {
			return Receipt{}, err
		}
	}
	return sourceReceipt(ctx, tx, input.request.SourceKey)
}

func encryptMessage(input enqueueInput, id string) ([]byte, error) {
	data := make(map[string]string, len(input.request.Data)+1)
	for key, value := range input.request.Data {
		data[key] = value
	}
	data["event.kind"] = input.request.EventKind
	payload, err := json.Marshal(model.NotificationPayload{Subject: input.request.Subject, Text: input.request.Text, Data: data})
	if err != nil || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidRequest
	}
	defer clear(payload)
	return input.access.Seal(payload, secretbox.NotificationMessageAAD(id))
}

func checkQueueCapacity(ctx context.Context, tx *sql.Tx, added int) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_deliveries WHERE state IN ('pending','leased')").Scan(&count); err != nil {
		return ErrQueueUnavailable
	}
	if count+added > MaximumPendingDeliveries {
		return ErrQueueCapacity
	}
	return nil
}

type selectedChannel struct {
	id       string
	revision int64
}

func selectChannels(ctx context.Context, tx *sql.Tx, input enqueueInput) (selected []selectedChannel, resultErr error) {
	if input.forceChannel != nil {
		return []selectedChannel{*input.forceChannel}, nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,current_revision,event_kinds_json FROM alert_channels WHERE enabled=1 AND deleted_at IS NULL ORDER BY id")
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	allowed := make(map[string]bool, len(input.request.ChannelIDs))
	for _, id := range input.request.ChannelIDs {
		allowed[id] = true
	}
	for rows.Next() {
		var channel selectedChannel
		var encoded string
		if err := rows.Scan(&channel.id, &channel.revision, &encoded); err != nil {
			return nil, ErrQueueUnavailable
		}
		var kinds []string
		if err := json.Unmarshal([]byte(encoded), &kinds); err != nil {
			return nil, ErrQueueUnavailable
		}
		if input.request.UseSubscriptions {
			for _, kind := range kinds {
				if kind == input.request.EventKind {
					allowed[channel.id] = true
				}
			}
		}
		if allowed[channel.id] && channel.id != input.excludeChannel {
			selected = append(selected, channel)
		}
		if len(selected) > MaximumChannels {
			return nil, ErrQueueCapacity
		}
	}
	return selected, rows.Err()
}

type newDelivery struct {
	messageID string
	channel   selectedChannel
	retryOf   string
}

func (service *Service) insertDelivery(ctx context.Context, tx *sql.Tx, input newDelivery) (string, error) {
	id, err := service.newID()
	if err != nil {
		return "", err
	}
	now := service.now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries
		(id,message_id,channel_id,channel_revision,state,next_attempt_at,retry_of,created_at,updated_at)
		VALUES(?,?,?,?,'pending',?,NULLIF(?,''),?,?)`, id, input.messageID, input.channel.id, input.channel.revision, now, input.retryOf, now, now)
	if err := exactlyOne(result, err); err != nil {
		return "", ErrQueueUnavailable
	}
	return id, nil
}
