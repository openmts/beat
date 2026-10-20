package remoteoperations

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) FinalizeTransfer(ctx context.Context, actor Actor, id string) (string, error) {
	return service.finalizeTransfer(ctx, scanAdmission{actor: actor, transferID: id})
}

func (tx transaction) createTransferTask(actor Actor, id string) (string, error) {
	creation := taskCreation{actor: actor, id: uuid.NewString()}
	input, err := tx.transferTaskInput(id)
	if err != nil {
		return "", err
	}
	creation.input = input
	creation.policy, err = tx.policy(input.PolicyID)
	if err != nil {
		return "", err
	}
	if err := tx.activeTaskQuota(input.PolicyID, creation.policy.MaximumActiveTasks); err != nil {
		return "", err
	}
	creation.nodes, err = tx.transferTargets(id)
	if err != nil {
		return "", err
	}
	creation.input.Limits = Limits{Kind: input.Limits.Kind, Targets: len(creation.nodes), TimeoutSeconds: creation.policy.TimeoutSeconds, OutputBytes: creation.policy.OutputBytes}
	for _, node := range creation.nodes {
		if err := tx.authorizeTarget(&creation, node); err != nil {
			return "", err
		}
	}
	creation.encoded, err = json.Marshal(creation.input)
	if err != nil {
		return "", ErrInvalid
	}
	defer clear(creation.encoded)
	if err := tx.persistTask(&creation); err != nil {
		return "", err
	}
	if err := tx.one("UPDATE operation_transfers SET task_id=?,state='queued',expires_at=?,updated_at=? WHERE id=?", creation.id, tx.now.Add(time.Duration(creation.policy.RetentionLimits().ArtifactHours)*time.Hour).UnixMilli(), tx.now.UnixMilli(), id); err != nil {
		return "", err
	}
	return creation.id, nil
}

func (tx transaction) transferTaskInput(id string) (TaskInput, error) {
	input := TaskInput{DedupeKey: id, File: &FileSpec{}}
	var sealed []byte
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT policy_id,direction,path_encrypted,root_id,byte_size,digest,mode,overwrite,artifact_id FROM operation_transfers WHERE id=?`, id).
		Scan(&input.PolicyID, &input.Limits.Kind, &sealed, &input.File.RootID, &input.File.Size, &input.File.Digest, &input.File.Mode, &input.File.Overwrite, &input.File.ArtifactID)
	if err != nil {
		return input, ErrUnavailable
	}
	plain, err := tx.access.Open(sealed, secretbox.OperationTransferAAD(id))
	if err != nil {
		return input, ErrUnavailable
	}
	defer clear(plain)
	input.File.RelativePath = string(plain)
	return input, nil
}

func (tx transaction) transferTargets(id string) (nodes []targetNode, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT node_id,node_name FROM operation_transfer_targets WHERE transfer_id=? ORDER BY node_id LIMIT 201", id)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var node targetNode
		if err := rows.Scan(&node.id, &node.name); err != nil {
			return nil, ErrUnavailable
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 || len(nodes) > MaximumTargets {
		return nil, ErrInvalid
	}
	return nodes, rows.Err()
}
