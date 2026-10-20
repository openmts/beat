package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

// MutateOperation 允许运维事务根据结果决定是否产生通知；状态、审计、outbox共享提交。
func (service *Service) MutateOperation(ctx context.Context, mutation func(*sql.Tx, secretbox.ResourceAccess) (*Request, error)) error {
	if mutation == nil {
		return ErrInvalidRequest
	}
	receipt := Receipt{Skipped: true}
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		request, err := mutation(tx, access)
		if err != nil {
			return err
		}
		if request != nil {
			receipt, err = service.persistOperationEvent(ctx, tx, enqueueInput{request: *request, access: access})
			if err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	service.recordEnqueueOutcome(receipt, err)
	if err != nil {
		return err
	}
	service.signal()
	return nil
}

func (service *Service) persistOperationEvent(ctx context.Context, tx *sql.Tx, input enqueueInput) (Receipt, error) {
	request := input.request
	if request.SourceKind != "operation" || (request.EventKind != "operation.completed" && request.EventKind != "operation.failed") {
		return Receipt{}, ErrInvalidRequest
	}
	if err := validateRequest(request); err != nil {
		return Receipt{}, err
	}
	if receipt, err := sourceReceipt(ctx, tx, request.SourceKey); err == nil {
		receipt.Duplicate = true
		return receipt, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Receipt{}, err
	}
	return service.enqueueTx(ctx, tx, input)
}
