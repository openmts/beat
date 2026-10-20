package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

type queryStore interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const messageDeliveryReceiptQuery = `SELECT id,channel_id,channel_revision,state,attempt_count FROM notification_deliveries AS d
	WHERE message_id=? AND NOT EXISTS(SELECT 1 FROM notification_deliveries AS retry WHERE retry.retry_of=d.id) ORDER BY channel_id`

func (service *Service) SourceReceipt(ctx context.Context, sourceKey string) (Receipt, error) {
	return sourceReceipt(ctx, service.db, sourceKey)
}

func sourceReceipt(ctx context.Context, database queryStore, sourceKey string) (receipt Receipt, resultErr error) {
	err := database.QueryRowContext(ctx, "SELECT id,routing_state FROM notification_messages WHERE source_key=?", sourceKey).Scan(&receipt.MessageID, &receipt.RoutingState)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, ErrQueueUnavailable
	}
	rows, err := database.QueryContext(ctx, messageDeliveryReceiptQuery, receipt.MessageID)
	if err != nil {
		return Receipt{}, ErrQueueUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	receipt.Deliveries = []model.NotificationDeliveryRef{}
	for rows.Next() {
		var ref model.NotificationDeliveryRef
		if err := rows.Scan(&ref.ID, &ref.ChannelID, &ref.ChannelRevision, &ref.State, &ref.AttemptCount); err != nil {
			return Receipt{}, ErrQueueUnavailable
		}
		receipt.Deliveries = append(receipt.Deliveries, ref)
		countDelivery(&receipt.Counts, ref.State)
	}
	receipt.State = receiptState(receipt)
	return receipt, rows.Err()
}

func countDelivery(counts *model.NotificationDeliveryCounts, state string) {
	switch state {
	case "pending":
		counts.Pending++
	case "leased":
		counts.Leased++
	case "succeeded":
		counts.Succeeded++
	case "dead":
		counts.Dead++
	case "cancelled":
		counts.Cancelled++
	}
}

func receiptState(receipt Receipt) string {
	if receipt.RoutingState == "not_routed" {
		return "not_routed"
	}
	counts := receipt.Counts
	if counts.Pending > 0 {
		return "pending"
	}
	if counts.Leased > 0 {
		return "leased"
	}
	if counts.Dead > 0 {
		return "dead"
	}
	if counts.Cancelled > 0 {
		return "cancelled"
	}
	return "succeeded"
}
