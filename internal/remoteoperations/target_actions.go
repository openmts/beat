package remoteoperations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/beat/backend/internal/secretbox"
)

type TargetRequest struct {
	TaskID   string `json:"-"`
	TargetID string `json:"-"`
}

type RetryInput struct {
	TargetRequest
	Reason                 string `json:"reason"`
	AcknowledgeUnknownRisk bool   `json:"acknowledge_unknown_risk"`
}

func (service *Service) RetryTarget(ctx context.Context, actor Actor, input RetryInput) error {
	if strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 || strings.ContainsAny(input.Reason, "\x00\r\n") {
		return ErrInvalid
	}
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		return tx.retryTarget(actor, input)
	})
}

func (tx transaction) retryTarget(actor Actor, input RetryInput) error {
	var state string
	var restoreRisk bool
	item := &dispatch{taskID: input.TaskID, targetID: input.TargetID}
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT n.state,COALESCE(n.node_id,''),t.policy_id,t.payload_encrypted,COALESCE(json_extract(t.policy_snapshot,'$.restore_risk'),0) FROM operation_task_targets n JOIN operation_tasks t ON t.id=n.task_id
		WHERE n.id=? AND t.id=?`, input.TargetID, input.TaskID).Scan(&state, &item.nodeID, &item.policyID, &item.payload, &restoreRisk)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	if item.nodeID == "" {
		return ErrConflict
	}
	if !slices.Contains([]string{"failed", "timed_out", "canceled", "output_limit", "interrupted", "expired", "unknown"}, state) {
		return ErrConflict
	}
	if (state == "unknown" || restoreRisk) && !input.AcknowledgeUnknownRisk {
		return ErrConflict
	}
	local, _, err := tx.capability(item.nodeID)
	if err != nil {
		return err
	}
	if err := tx.validateRetry(item, local); err != nil {
		return err
	}
	if err := tx.one(`UPDATE operation_task_targets SET state='queued',current_attempt='',retry_actor=?,retry_reason=?,updated_at=? WHERE id=?`, actor.UserID, input.Reason, tx.now.UnixMilli(), input.TargetID); err != nil {
		return err
	}
	if err := tx.one("UPDATE operation_tasks SET cancel_requested=0,expires_at=?,updated_at=? WHERE id=?", tx.now.Add(QueueLifetime).UnixMilli(), tx.now.UnixMilli(), input.TaskID); err != nil {
		return err
	}
	return tx.audit(actor.UserID, "operation.target.retried", input.TargetID)
}

func (tx transaction) validateRetry(item *dispatch, local Policy) error {
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT current_attempt FROM operation_task_targets WHERE id=?", item.targetID).Scan(&item.ignoreAttempt); err != nil {
		return ErrUnavailable
	}
	return tx.validateDispatch(item, local)
}

type TargetOutput struct {
	AttemptID string `json:"attempt_id"`
	Result
}

func (service *Service) TargetOutput(ctx context.Context, actor Actor, input TargetRequest) (TargetOutput, error) {
	var result TargetOutput
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		var err error
		result, err = tx.targetOutput(input)
		if err != nil {
			return err
		}
		return tx.audit(actor.UserID, "operation.output.viewed", input.TargetID)
	})
	if err != nil {
		clear(result.Stdout)
		clear(result.Stderr)
		return TargetOutput{}, err
	}
	return result, nil
}

func (tx transaction) targetOutput(input TargetRequest) (TargetOutput, error) {
	var id, metadata string
	var stdout, stderr []byte
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT a.id,a.result_json,a.stdout_encrypted,a.stderr_encrypted FROM operation_task_targets n
		JOIN operation_attempts a ON a.id=n.current_attempt WHERE n.id=? AND n.task_id=? AND a.completed_at IS NOT NULL`, input.TargetID, input.TaskID).Scan(&id, &metadata, &stdout, &stderr)
	if errors.Is(err, sql.ErrNoRows) {
		return TargetOutput{}, ErrNotFound
	}
	if err != nil {
		return TargetOutput{}, ErrUnavailable
	}
	var result TargetOutput
	result.AttemptID = id
	if len(stdout) == 0 && len(stderr) == 0 {
		return TargetOutput{}, ErrNotFound
	}
	if json.Unmarshal([]byte(metadata), &result.Result) != nil {
		return TargetOutput{}, ErrUnavailable
	}
	result.Stdout, err = tx.access.Open(stdout, secretbox.OperationOutputAAD(id, "stdout"))
	if err != nil {
		return TargetOutput{}, ErrUnavailable
	}
	result.Stderr, err = tx.access.Open(stderr, secretbox.OperationOutputAAD(id, "stderr"))
	if err != nil {
		clear(result.Stdout)
		return TargetOutput{}, ErrUnavailable
	}
	return result, nil
}
