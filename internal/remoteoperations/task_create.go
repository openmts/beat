package remoteoperations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

const QueueLifetime = 24 * time.Hour

type taskCreation struct {
	actor   Actor
	input   TaskInput
	encoded []byte
	id      string
	policy  Policy
	nodes   []targetNode
}

type targetNode struct{ id, name string }

func (service *Service) CreateTask(ctx context.Context, actor Actor, input TaskInput) (string, error) {
	if input.ShellFamily == "" {
		input.ShellFamily = "sh"
	}
	if input.ShellFamily != "sh" && input.ShellFamily != "powershell" {
		return "", ErrInvalid
	}
	if !validID(input.DedupeKey) || ValidateCommand(input.Command) != nil || (input.Limits.Kind != "shell" && input.Limits.Kind != "snippet") {
		return "", ErrInvalid
	}
	if len(input.NodeIDs)+len(input.GroupIDs) < 1 || len(input.NodeIDs)+len(input.GroupIDs) > MaximumTargets {
		return "", ErrInvalid
	}
	if input.File != nil || input.Limits.Targets < 0 || input.Limits.Targets > MaximumTargets {
		return "", ErrInvalid
	}
	input.Limits.Targets = 0
	input.NodeIDs = slices.Clone(input.NodeIDs)
	input.GroupIDs = slices.Clone(input.GroupIDs)
	slices.Sort(input.NodeIDs)
	slices.Sort(input.GroupIDs)
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", ErrInvalid
	}
	defer clear(encoded)
	creation := taskCreation{actor: actor, input: input, encoded: encoded, id: uuid.NewString()}
	err = service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		duplicate, err := tx.existingTask(&creation)
		if err != nil || duplicate {
			return err
		}
		return tx.createTask(&creation)
	})
	return creation.id, err
}

func (tx transaction) existingTask(creation *taskCreation) (bool, error) {
	var id, actor string
	var sealed []byte
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT id,actor_id,payload_encrypted FROM operation_tasks WHERE dedupe_key=?", creation.input.DedupeKey).Scan(&id, &actor, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrUnavailable
	}
	if actor != creation.actor.UserID {
		return false, ErrConflict
	}
	plain, err := tx.access.Open(sealed, secretbox.OperationTaskAAD(id))
	if err != nil {
		return false, ErrUnavailable
	}
	defer clear(plain)
	var stored TaskInput
	if json.Unmarshal(plain, &stored) != nil {
		return false, ErrUnavailable
	}
	stored.Limits.Targets = 0
	canonical, marshalErr := json.Marshal(stored)
	if marshalErr != nil {
		return false, ErrUnavailable
	}
	defer clear(canonical)
	if !bytes.Equal(canonical, creation.encoded) {
		return false, ErrConflict
	}
	creation.id = id
	return true, nil
}

func (tx transaction) createTask(creation *taskCreation) error {
	if err := tx.validateTaskSnippet(creation); err != nil {
		return err
	}
	policy, err := tx.policy(creation.input.PolicyID)
	if err != nil {
		return err
	}
	creation.policy = policy
	nodes, err := tx.resolveTargets(creation.input)
	if err != nil {
		return err
	}
	creation.nodes = nodes
	if len(nodes) == 0 || len(nodes) > policy.MaximumTargets {
		return ErrForbidden
	}
	for _, node := range nodes {
		if err := tx.authorizeTarget(creation, node); err != nil {
			return err
		}
	}
	if err := tx.activeTaskQuota(creation.input.PolicyID, policy.MaximumActiveTasks); err != nil {
		return err
	}
	creation.input.Limits.Targets = len(creation.nodes)
	clear(creation.encoded)
	creation.encoded, err = json.Marshal(creation.input)
	if err != nil {
		return ErrInvalid
	}
	defer clear(creation.encoded)
	if err := tx.persistTask(creation); err != nil {
		return err
	}
	return tx.audit(creation.actor.UserID, "operation.task.created", creation.id)
}

func (tx transaction) authorizeTarget(creation *taskCreation, node targetNode) error {
	if err := tx.scope(creation.input.PolicyID, node.id); err != nil {
		return err
	}
	local, _, err := tx.capability(node.id)
	if err != nil {
		return err
	}
	limits := creation.input.Limits
	limits.Targets = len(creation.nodes)
	if limits.Kind == "shell" || limits.Kind == "snippet" {
		if err := AuthorizeShell(creation.policy, local, creation.input.ShellFamily); err != nil {
			return err
		}
	}
	if err := Authorize(creation.policy, local, limits); err != nil {
		return err
	}
	if creation.input.File != nil {
		return AuthorizeFile(creation.policy, local, FileRequest{Direction: limits.Kind, File: *creation.input.File})
	}
	return nil
}

func (tx transaction) resolveTargets(input TaskInput) (nodes []targetNode, resultErr error) {
	if err := tx.validateSelections(input); err != nil {
		return nil, err
	}
	nodeJSON, err := json.Marshal(input.NodeIDs)
	if err != nil {
		return nil, ErrInvalid
	}
	groupJSON, err := json.Marshal(input.GroupIDs)
	if err != nil {
		return nil, ErrInvalid
	}
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT id,name FROM nodes WHERE id IN (SELECT value FROM json_each(?))
		OR group_id IN (SELECT value FROM json_each(?)) ORDER BY id LIMIT ?`, string(nodeJSON), string(groupJSON), MaximumTargets+1)
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
	return nodes, rows.Err()
}

func (tx transaction) persistTask(creation *taskCreation) error {
	sealed, err := tx.access.Seal(creation.encoded, secretbox.OperationTaskAAD(creation.id))
	if err != nil {
		return ErrUnavailable
	}
	digest, err := fingerprint(tx.access, creation.encoded)
	if err != nil {
		return err
	}
	policy, err := json.Marshal(struct {
		Policy
		ShellFamily string `json:"task_shell_family,omitempty"`
		RunAsRoot   bool   `json:"task_run_as_root,omitempty"`
	}{Policy: creation.policy, ShellFamily: creation.input.ShellFamily, RunAsRoot: creation.input.Limits.RunAsRoot})
	if err != nil {
		return ErrInvalid
	}
	input := creation.input
	if err := tx.one(`INSERT INTO operation_tasks(id,dedupe_key,request_fingerprint,kind,payload_encrypted,policy_id,policy_snapshot,actor_id,
		timeout_seconds,output_bytes,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, creation.id, input.DedupeKey, digest, input.Limits.Kind, sealed,
		input.PolicyID, string(policy), creation.actor.UserID, input.Limits.TimeoutSeconds, input.Limits.OutputBytes, tx.now.Add(time.Duration(creation.policy.RetentionLimits().QueueHours)*time.Hour).UnixMilli(), tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return err
	}
	for _, node := range creation.nodes {
		if err := tx.one(`INSERT INTO operation_task_targets(id,task_id,node_id,node_name,state,created_at,updated_at) VALUES(?,?,?,?,'queued',?,?)`,
			uuid.NewString(), creation.id, node.id, node.name, tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

func (tx transaction) activeTaskQuota(policyID string, maximum int) error {
	var active int
	if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(DISTINCT t.id) FROM operation_tasks t JOIN operation_task_targets n ON n.task_id=t.id
 WHERE t.policy_id=? AND n.node_id IS NOT NULL AND n.state IN ('queued','leased','running','unknown','cancel_requested')`, policyID).Scan(&active); err != nil {
		return ErrUnavailable
	}
	if active >= maximum {
		return ErrForbidden
	}
	return nil
}
