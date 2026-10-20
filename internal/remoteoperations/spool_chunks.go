package remoteoperations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

type ChunkInput struct {
	Total      int64
	TransferID string
	Index      int
	Digest     string
	Content    []byte
}

type artifact struct {
	id, direction, state, digest, scanStatus string
	size, expiry                             int64
	requireScan                              bool
}

func (tx transaction) artifact(transferID string) (artifact, error) {
	var item artifact
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT a.id,a.direction,a.state,a.digest,a.byte_size,a.expires_at,(t.require_scan OR COALESCE(json_extract(p.configuration_json,'$.require_scan'),0)),a.scan_status FROM operation_artifacts a
		JOIN operation_transfers t ON t.artifact_id=a.id LEFT JOIN operation_policies p ON p.id=t.policy_id WHERE t.id=?`, transferID).Scan(&item.id, &item.direction, &item.state, &item.digest, &item.size, &item.expiry, &item.requireScan, &item.scanStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, ErrUnavailable
	}
	if item.expiry <= tx.now.UnixMilli() {
		return item, ErrConflict
	}
	return item, nil
}

func (service *Service) UploadChunk(ctx context.Context, actor Actor, input ChunkInput) error {
	if service.spool == nil {
		return ErrUnavailable
	}
	var outcome uploadOutcome
	err := service.operationMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, false, true); err != nil {
			return nil, err
		}
		item, err := tx.artifact(input.TransferID)
		if err != nil {
			return nil, err
		}
		if item.direction != "deploy" {
			return nil, ErrForbidden
		}
		var event *notificationqueue.Request
		outcome, event, err = tx.uploadMutation(service.spool, uploadMutationInput{actor: actor, item: item, chunk: input})
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

func (tx transaction) uploadChunk(spool *Spool, item artifact, input ChunkInput) (string, error) {
	if item.state != "uploading" && item.state != "scanning" && item.state != "ready" {
		return "", ErrConflict
	}
	if err := validateChunk(item, input); err != nil {
		return "", err
	}
	var digest string
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT digest FROM operation_artifact_chunks WHERE artifact_id=? AND chunk_index=?", item.id, input.Index).Scan(&digest)
	if err == nil {
		if digest == input.Digest {
			plain, readErr := tx.readChunk(spool, item, input.Index)
			clear(plain)
			if readErr != nil {
				return "", errArtifactCorrupt
			}
			return "", nil
		}
		return "", ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnavailable
	}
	if item.state == "scanning" {
		return "", ErrBusy
	}
	if item.state != "uploading" {
		return "", ErrConflict
	}
	access, ok := tx.access.(secretbox.OperationSpoolAccess)
	if !ok {
		return "", ErrUnavailable
	}
	sealed, err := access.SealOperationFrame(input.Content, secretbox.OperationFrame{ArtifactID: item.id, Direction: item.direction, Index: input.Index, TotalBytes: item.size, Digest: item.digest})
	if err != nil {
		return "", ErrUnavailable
	}
	keyID, err := secretbox.OperationFrameKeyID(sealed)
	if err != nil {
		return "", ErrUnavailable
	}
	name := uuid.NewString()
	if err := spool.write(name, sealed); err != nil {
		return name, ErrUnavailable
	}
	err = tx.one(`INSERT INTO operation_artifact_chunks(id,artifact_id,chunk_index,storage_name,data_key_id,byte_size,digest,created_at) VALUES(?,?,?,?,?,?,?,?)`, uuid.NewString(), item.id, input.Index, name, keyID, len(input.Content), input.Digest, tx.now.UnixMilli())
	return name, err
}

func validateChunk(item artifact, input ChunkInput) error {
	if input.Total != 0 && input.Total != item.size {
		return ErrInvalid
	}
	if input.Index < 0 || input.Index >= 4096 || int64(input.Index)*SpoolChunkBytes >= item.size {
		return ErrInvalid
	}
	expected := min(int64(SpoolChunkBytes), item.size-int64(input.Index)*SpoolChunkBytes)
	if int64(len(input.Content)) != expected {
		return ErrInvalid
	}
	digest := sha256.Sum256(input.Content)
	if input.Digest != hex.EncodeToString(digest[:]) {
		return ErrInvalid
	}
	return nil
}
