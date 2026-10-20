package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

const MaximumBatchMessages = 2

type BatchEnqueuer interface {
	EnqueueBatch(context.Context, []Request, func(*sql.Tx) error) ([]Receipt, error)
}

type notificationBatch struct {
	requests []Request
	mutation func(*sql.Tx) error
}

func (service *Service) EnqueueBatch(ctx context.Context, requests []Request, mutation func(*sql.Tx) error) ([]Receipt, error) {
	if err := validateNotificationBatch(requests); err != nil {
		return nil, err
	}
	var receipts []Receipt
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		var err error
		receipts, err = service.enqueueBatchWithAccess(ctx, notificationBatch{requests: requests, mutation: mutation}, access)
		return err
	})
	if errors.Is(err, ErrSkipMutation) {
		return skippedBatchReceipts(len(requests)), nil
	}
	if err != nil {
		service.recordEnqueueOutcome(Receipt{}, err)
		return nil, err
	}
	service.recordEnqueueOutcome(receipts[0], nil)
	service.signal()
	return receipts, nil
}

func validateNotificationBatch(requests []Request) error {
	if len(requests) < 1 || len(requests) > MaximumBatchMessages {
		return ErrInvalidRequest
	}
	keys := make(map[string]bool, len(requests))
	for _, request := range requests {
		if err := validateRequest(request); err != nil {
			return err
		}
		if keys[request.SourceKey] {
			return ErrInvalidRequest
		}
		keys[request.SourceKey] = true
	}
	return nil
}

func (service *Service) enqueueBatchWithAccess(ctx context.Context, batch notificationBatch, access secretbox.ResourceAccess) ([]Receipt, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	existing, count, err := existingBatchReceipts(ctx, tx, batch.requests)
	if err != nil {
		return nil, err
	}
	if count == len(batch.requests) {
		return existing, nil
	}
	if count != 0 {
		return nil, resolveMixedBatch(tx, batch.mutation)
	}
	if err := preflightBatchCapacity(ctx, tx, batch.requests); err != nil {
		return nil, err
	}
	if batch.mutation != nil {
		if err := batch.mutation(tx); err != nil {
			return nil, err
		}
	}
	receipts, err := service.persistNotificationBatch(ctx, tx, batchPersistence{requests: batch.requests, access: access})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, ErrQueueUnavailable
	}
	return receipts, nil
}

func resolveMixedBatch(tx *sql.Tx, mutation func(*sql.Tx) error) error {
	if mutation == nil {
		return ErrConflict
	}
	err := mutation(tx)
	if errors.Is(err, ErrSkipMutation) {
		return ErrSkipMutation
	}
	return errors.Join(ErrConflict, err)
}

func skippedBatchReceipts(count int) []Receipt {
	receipts := make([]Receipt, count)
	for index := range receipts {
		receipts[index] = Receipt{Skipped: true, Deliveries: []model.NotificationDeliveryRef{}}
	}
	return receipts
}
