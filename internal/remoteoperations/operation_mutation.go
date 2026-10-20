package remoteoperations

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

type operationMutator interface {
	MutateOperation(context.Context, func(*sql.Tx, secretbox.ResourceAccess) (*notificationqueue.Request, error)) error
}

func (service *Service) operationMutation(ctx context.Context, mutate func(transaction) (*notificationqueue.Request, error)) error {
	if !service.enabled {
		return ErrForbidden
	}
	return service.atomicMutation(ctx, mutate)
}

func (service *Service) atomicMutation(ctx context.Context, mutate func(transaction) (*notificationqueue.Request, error)) error {
	queue, ok := service.queue.(operationMutator)
	if !ok {
		return ErrUnavailable
	}
	return queue.MutateOperation(ctx, func(db *sql.Tx, access secretbox.ResourceAccess) (*notificationqueue.Request, error) {
		return mutate(service.transaction(ctx, db, access))
	})
}

func operationFailure(key, id string) *notificationqueue.Request {
	return &notificationqueue.Request{SourceKey: "operation-transfer:" + key, SourceKind: "operation", SourceID: id, EventKind: "operation.failed",
		Subject: "Remote operation stopped", Text: "A remote operation requires attention in Beat.", UseSubscriptions: true}
}

type uploadOutcome struct {
	scanID   string
	created  string
	conflict bool
}

func (tx transaction) uploadMutation(spool *Spool, input uploadMutationInput) (uploadOutcome, *notificationqueue.Request, error) {
	name, err := tx.uploadChunk(spool, input.item, input.chunk)
	if err == errArtifactCorrupt || err == ErrConflict && input.item.state == "uploading" {
		scanID, stopErr := tx.stopTransfer(input.actor, input.chunk.TransferID, "corrupt")
		if stopErr != nil {
			return uploadOutcome{}, nil, stopErr
		}
		return uploadOutcome{conflict: true, scanID: scanID}, operationFailure("chunk-conflict:"+input.chunk.TransferID, input.chunk.TransferID), nil
	}
	if err != nil {
		return uploadOutcome{created: name}, nil, err
	}
	if name != "" && input.item.direction == "collect" {
		if err := tx.exec(`UPDATE operation_transfer_targets SET progress_bytes=(SELECT COALESCE(SUM(byte_size),0) FROM operation_artifact_chunks WHERE artifact_id=?),updated_at=? WHERE transfer_id=?`, input.item.id, tx.now.UnixMilli(), input.chunk.TransferID); err != nil {
			return uploadOutcome{created: name}, nil, err
		}
	}
	if name != "" {
		err = tx.audit(input.actor.UserID, "operation.transfer.chunk.received", input.chunk.TransferID)
	}
	return uploadOutcome{created: name}, nil, err
}

type uploadMutationInput struct {
	actor Actor
	item  artifact
	chunk ChunkInput
}
