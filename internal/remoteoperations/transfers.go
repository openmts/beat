package remoteoperations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

const UploadLifetime = 6 * time.Hour

const ArtifactLifetime = 24 * time.Hour

type TransferInput struct {
	Direction string   `json:"direction"`
	Filename  string   `json:"filename"`
	PolicyID  string   `json:"policy_id"`
	NodeIDs   []string `json:"node_ids"`
	GroupIDs  []string `json:"group_ids"`
	File      FileSpec `json:"file"`
}

type TransferSummary struct {
	DownloadAvailable bool             `json:"download_available"`
	Targets           []TransferTarget `json:"targets,omitempty"`
	ID                string           `json:"id"`
	Direction         string           `json:"direction"`
	State             string           `json:"state"`
	Size              int64            `json:"size"`
	Digest            string           `json:"digest"`
	TaskID            string           `json:"task_id,omitempty"`
	ExpiresAt         int64            `json:"expires_at"`
	MissingChunks     []int            `json:"missing_chunks,omitempty"`
}

type transferCreation struct {
	existing                bool
	input                   TransferInput
	id, artifactID, actorID string
	nodes                   []targetNode
	policy                  Policy
}

func (service *Service) CreateTransfer(ctx context.Context, actor Actor, input TransferInput) (TransferSummary, error) {
	if service.spool == nil {
		return TransferSummary{}, ErrUnavailable
	}
	if input.Direction == "collect" {
		empty := sha256.Sum256(nil)
		input.File.Size = 0
		input.File.Digest = hex.EncodeToString(empty[:])
		input.File.Mode = 0o600
		input.File.Overwrite = false
	}
	if input.Filename == "" {
		input.Filename = path.Base(input.File.RelativePath)
	}

	if (input.Direction != "deploy" && input.Direction != "collect") || ValidateFile(input.File) != nil || input.Filename == "" || len(input.Filename) > 255 || path.Base(input.Filename) != input.Filename || ValidateRelativePath(input.Filename) != nil || strings.ContainsAny(input.Filename, "\\\x00\r\n") {
		return TransferSummary{}, ErrInvalid
	}
	creation := transferCreation{input: input, id: uuid.NewString(), artifactID: uuid.NewString(), actorID: actor.UserID}
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		if err := tx.prepareTransfer(&creation, service.spool); err != nil {
			return err
		}
		if err := tx.persistTransfer(actor, creation); err != nil {
			return err
		}
		if input.Direction == "collect" {
			if err := tx.one("UPDATE operation_artifacts SET state='awaiting_source',expires_at=? WHERE id=?", tx.now.Add(time.Duration(creation.policy.RetentionLimits().QueueHours)*time.Hour).UnixMilli(), creation.artifactID); err != nil {
				return err
			}
			_, err := tx.createTransferTask(actor, creation.id)
			return err
		}
		return nil
	})
	if err != nil {
		return TransferSummary{}, err
	}
	return service.GetTransfer(ctx, actor, creation.id)
}

func (tx transaction) prepareTransfer(creation *transferCreation, spool *Spool) error {
	policy, err := tx.policy(creation.input.PolicyID)
	if err != nil {
		return err
	}
	creation.policy = policy
	if policy.RequireScan && spool.scanner == nil {
		return ErrForbidden
	}
	nodes, err := tx.resolveTargets(TaskInput{NodeIDs: creation.input.NodeIDs, GroupIDs: creation.input.GroupIDs})
	if err != nil {
		return err
	}
	creation.nodes = nodes
	if len(nodes) == 0 || len(nodes) > policy.MaximumTargets || creation.input.Direction == "collect" && len(nodes) != 1 {
		return ErrForbidden
	}
	input := TaskInput{PolicyID: creation.input.PolicyID, File: &creation.input.File, Limits: Limits{Kind: creation.input.Direction, Targets: len(nodes), TimeoutSeconds: policy.TimeoutSeconds, OutputBytes: policy.OutputBytes}}
	task := &taskCreation{input: input, policy: policy, nodes: nodes}
	for _, node := range nodes {
		if err := tx.authorizeTarget(task, node); err != nil {
			return err
		}
	}
	return tx.transferQuota(creation, spool)
}

func (tx transaction) persistTransfer(actor Actor, creation transferCreation) error {
	metadata, err := json.Marshal(map[string]string{"filename": creation.input.Filename})
	if err != nil {
		return ErrInvalid
	}
	sealed, err := tx.access.Seal(metadata, secretbox.OperationArtifactAAD(creation.artifactID))
	if err != nil {
		return ErrUnavailable
	}
	file := creation.input.File
	if err := tx.one(`INSERT INTO operation_artifacts(id,storage_name,metadata_encrypted,direction,byte_size,digest,scan_status,state,actor_id,expires_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'pending','uploading',?,?,?,?)`, creation.artifactID, uuid.NewString(), sealed, creation.input.Direction, file.Size, file.Digest, actor.UserID, tx.now.Add(time.Duration(creation.policy.RetentionLimits().UploadHours)*time.Hour).UnixMilli(), tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return err
	}
	path, err := tx.access.Seal([]byte(file.RelativePath), secretbox.OperationTransferAAD(creation.id))
	if err != nil {
		return ErrUnavailable
	}
	if err := tx.one(`INSERT INTO operation_transfers(id,policy_id,require_scan,direction,path_encrypted,root_id,byte_size,digest,mode,overwrite,artifact_id,state,actor_id,expires_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'uploading',?,?,?,?)`, creation.id, creation.input.PolicyID, creation.policy.RequireScan, creation.input.Direction, path, file.RootID, file.Size, file.Digest, file.Mode, file.Overwrite, creation.artifactID, actor.UserID, tx.now.Add(time.Duration(creation.policy.RetentionLimits().UploadHours)*time.Hour).UnixMilli(), tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return err
	}
	for _, node := range creation.nodes {
		if err := tx.one(`INSERT INTO operation_transfer_targets(id,transfer_id,node_id,node_name,state,created_at,updated_at) VALUES(?,?,?,?,'queued',?,?)`, uuid.NewString(), creation.id, node.id, node.name, tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.audit(actor.UserID, "operation.transfer.created", creation.id)
}
