package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

func existingBatchReceipts(ctx context.Context, tx *sql.Tx, requests []Request) ([]Receipt, int, error) {
	receipts := make([]Receipt, len(requests))
	count := 0
	for index, request := range requests {
		receipt, err := sourceReceipt(ctx, tx, request.SourceKey)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		receipt.Duplicate = true
		receipts[index] = receipt
		count++
	}
	return receipts, count, nil
}

func preflightBatchCapacity(ctx context.Context, tx *sql.Tx, requests []Request) error {
	total := 0
	for _, request := range requests {
		channels, err := selectChannels(ctx, tx, enqueueInput{request: request})
		if err != nil {
			return err
		}
		total += len(channels)
	}
	return checkQueueCapacity(ctx, tx, total)
}

type batchPersistence struct {
	requests []Request
	access   secretbox.ResourceAccess
}

func (service *Service) persistNotificationBatch(ctx context.Context, tx *sql.Tx, input batchPersistence) ([]Receipt, error) {
	receipts := make([]Receipt, 0, len(input.requests))
	for _, request := range input.requests {
		receipt, err := service.enqueueTx(ctx, tx, enqueueInput{request: request, access: input.access})
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}
