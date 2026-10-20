package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

// EnqueueOperation 将运维结果加密、状态与审计回调放进 outbox 的同一事务。
// 重复请求仍执行回调进行当前身份与结果一致性复验。
func (service *Service) EnqueueOperation(ctx context.Context, request Request, mutation func(*sql.Tx, secretbox.ResourceAccess) error) (Receipt, error) {
	if request.SourceKind != "operation" || (request.EventKind != "operation.completed" && request.EventKind != "operation.failed") || mutation == nil {
		return Receipt{}, ErrInvalidRequest
	}
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
		if err := mutation(tx, access); err != nil {
			return err
		}
		receipt, err = sourceReceipt(ctx, tx, request.SourceKey)
		if err == nil {
			receipt.Duplicate = true
		} else if errors.Is(err, ErrNotFound) {
			receipt, err = service.enqueueTx(ctx, tx, enqueueInput{request: request, access: access})
		}
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	})
	service.recordEnqueueOutcome(receipt, err)
	if err != nil {
		return Receipt{}, err
	}
	service.signal()
	return receipt, nil
}
