package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/notificationqueue"
)

type AgentChunk struct {
	Total   int64
	Attempt AttemptRequest
	Index   int
	Digest  string
	Content []byte
}

func (tx transaction) attemptArtifact(request AttemptRequest) (artifact, string, error) {
	attempt, err := tx.loadAttempt(request)
	if err != nil {
		return artifact{}, "", err
	}
	if err := attempt.attempt.Authenticate(Proof{NodeID: attempt.attempt.NodeID, Nonce: request.Nonce, Now: tx.now}); err != nil {
		return artifact{}, "", err
	}
	if attempt.attempt.State != "running" || attempt.attempt.StartedAt == nil || attempt.attempt.CompletedAt != nil {
		return artifact{}, "", ErrConflict
	}
	var transferID, current string
	err = tx.tx.QueryRowContext(tx.ctx, `SELECT t.id,n.current_attempt FROM operation_transfers t JOIN operation_task_targets n ON n.task_id=t.task_id WHERE t.task_id=? AND n.id=?`, attempt.taskID, attempt.targetID).Scan(&transferID, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return artifact{}, "", ErrNotFound
	}
	if err != nil {
		return artifact{}, "", ErrUnavailable
	}
	if current != request.ID {
		return artifact{}, "", ErrConflict
	}
	item, err := tx.artifact(transferID)
	return item, transferID, err
}

func (service *Service) CollectSource(ctx context.Context, request AttemptRequest, source SourceInfo) error {
	if service.spool == nil {
		return ErrUnavailable
	}
	return service.write(ctx, func(tx transaction) error {
		item, id, err := tx.attemptArtifact(request)
		if err != nil {
			return err
		}
		if item.direction != "collect" {
			return ErrForbidden
		}
		if (item.state == "uploading" || item.state == "scanning" || item.state == "ready") && source.Size == item.size && source.Digest == item.digest {
			return nil
		}
		if item.state != "awaiting_source" {
			return ErrConflict
		}
		input, err := tx.transferTaskInput(id)
		if err != nil {
			return err
		}
		input.File.Size = source.Size
		input.File.Digest = source.Digest
		creation := transferCreation{existing: true, input: TransferInput{Direction: "collect", PolicyID: input.PolicyID, File: *input.File}}
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT actor_id FROM operation_transfers WHERE id=?", id).Scan(&creation.actorID); err != nil {
			return ErrUnavailable
		}
		nodes, err := tx.transferTargets(id)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			creation.input.NodeIDs = append(creation.input.NodeIDs, node.id)
		}
		if err := tx.prepareTransfer(&creation, service.spool); err != nil {
			return err
		}
		if err := tx.one("UPDATE operation_artifacts SET state='uploading',byte_size=?,digest=?,expires_at=?,updated_at=? WHERE id=?", source.Size, source.Digest, tx.now.Add(time.Duration(creation.policy.RetentionLimits().UploadHours)*time.Hour).UnixMilli(), tx.now.UnixMilli(), item.id); err != nil {
			return err
		}
		return tx.one("UPDATE operation_transfers SET byte_size=?,digest=?,updated_at=? WHERE id=?", source.Size, source.Digest, tx.now.UnixMilli(), id)
	})
}

func (service *Service) UploadAgentChunk(ctx context.Context, input AgentChunk) error {
	if service.spool == nil {
		return ErrUnavailable
	}
	var outcome uploadOutcome
	err := service.operationMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		item, id, err := tx.attemptArtifact(input.Attempt)
		if err != nil {
			return nil, err
		}
		if item.direction != "collect" {
			return nil, ErrForbidden
		}
		chunk := ChunkInput{TransferID: id, Index: input.Index, Digest: input.Digest, Total: input.Total, Content: input.Content}
		var event *notificationqueue.Request
		outcome, event, err = tx.uploadMutation(service.spool, uploadMutationInput{item: item, chunk: chunk})
		return event, err
	})
	if err != nil && outcome.created != "" {
		return errors.Join(err, service.spool.remove(outcome.created))
	}
	if err == nil && outcome.conflict {
		service.stopScan(outcome.scanID)
		return ErrConflict
	}
	return err
}

func (service *Service) DownloadAgentChunk(ctx context.Context, input AgentChunk) ([]byte, error) {
	if service.spool == nil {
		return nil, ErrUnavailable
	}
	var outcome chunkReadOutcome
	err := service.operationMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		item, id, err := tx.attemptArtifact(input.Attempt)
		if err != nil {
			return nil, err
		}
		if item.state == "scanning" || item.state == "ready" && item.requireScan && item.scanStatus != "clean" {
			return nil, ErrBusy
		}
		if item.direction != "deploy" || item.state != "ready" {
			return nil, ErrForbidden
		}
		if input.Index < 0 || int64(input.Index)*SpoolChunkBytes >= item.size {
			return nil, ErrInvalid
		}
		var event *notificationqueue.Request
		outcome, event, err = tx.readChunkMutation(service.spool, guardedChunk{item: item, transferID: id, index: input.Index})
		return event, err
	})
	if err != nil {
		clear(outcome.content)
		return nil, err
	}
	if outcome.corrupt {
		return nil, ErrConflict
	}
	return outcome.content, nil
}

func (service *Service) FinalizeCollect(ctx context.Context, request AttemptRequest) error {
	_, err := service.finalizeTransfer(ctx, scanAdmission{agent: &request})
	return err
}

func (service *Service) AgentFileInfo(ctx context.Context, request AttemptRequest) (SourceInfo, error) {
	var info SourceInfo
	err := service.write(ctx, func(tx transaction) error {
		item, _, err := tx.attemptArtifact(request)
		if err != nil {
			return err
		}
		info = SourceInfo{Size: item.size, Digest: item.digest}
		return nil
	})
	return info, err
}
