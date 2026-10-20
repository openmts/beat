package notificationqueue

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

type LifecycleMutation struct {
	Apply    func(*sql.Tx) (*Request, error)
	Complete func(*sql.Tx, Receipt) error
}

// MutateLifecycle 将日期、审计、事件与outbox路由结果原子提交，重复source也先复验权限。
func (service *Service) MutateLifecycle(ctx context.Context, mutation LifecycleMutation) (Receipt, error) {
	if mutation.Apply == nil {
		return Receipt{}, ErrInvalidRequest
	}
	receipt := Receipt{Skipped: true}
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrQueueUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		request, err := mutation.Apply(tx)
		if err != nil {
			return err
		}
		if request != nil {
			receipt, err = service.persistLifecycleEvent(ctx, tx, enqueueInput{request: *request, access: access})
			if err != nil {
				return err
			}
		}
		if mutation.Complete != nil {
			if err = mutation.Complete(tx, receipt); err != nil {
				return err
			}
		}
		if err = tx.Commit(); err != nil {
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

func (service *Service) persistLifecycleEvent(ctx context.Context, tx *sql.Tx, input enqueueInput) (Receipt, error) {
	request := input.request
	if request.SourceKind != "node_lifecycle" || (request.EventKind != "node.expiring" && request.EventKind != "node.expired" && request.EventKind != "node.renewed") {
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
