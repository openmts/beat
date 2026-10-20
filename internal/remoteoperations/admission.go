package remoteoperations

import "encoding/json"

func (tx transaction) scope(policyID, nodeID string) error {
	var count int
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM nodes n WHERE n.id=? AND n.agent_token_created_at IS NOT NULL
		AND n.agent_token_revoked_at IS NULL AND (EXISTS(SELECT 1 FROM operation_policy_nodes p WHERE p.policy_id=? AND p.node_id=n.id)
		OR EXISTS(SELECT 1 FROM operation_policy_groups p WHERE p.policy_id=? AND p.group_id=n.group_id))`, nodeID, policyID, policyID).Scan(&count)
	if err != nil {
		return ErrUnavailable
	}
	if count != 1 {
		return ErrForbidden
	}
	return nil
}

func (tx transaction) validateSelections(input TaskInput) error {
	for _, selection := range []struct {
		ids   []string
		query string
	}{
		{input.NodeIDs, "SELECT COUNT(*) FROM nodes WHERE id IN (SELECT value FROM json_each(?))"},
		{input.GroupIDs, "SELECT COUNT(*) FROM groups WHERE id IN (SELECT value FROM json_each(?))"},
	} {
		encoded, err := json.Marshal(selection.ids)
		if err != nil {
			return ErrInvalid
		}
		var count int
		if err := tx.tx.QueryRowContext(tx.ctx, selection.query, string(encoded)).Scan(&count); err != nil {
			return ErrUnavailable
		}
		if count != len(selection.ids) {
			return ErrInvalid
		}
	}
	return nil
}

func (tx transaction) authorizeStart(item storedAttempt, request AttemptRequest) error {
	if err := item.attempt.Authenticate(Proof{NodeID: item.attempt.NodeID, Nonce: request.Nonce, Now: tx.now}); err != nil {
		return err
	}
	if item.attempt.State == "canceled" || item.attempt.State == "cancel_requested" || item.attempt.CompletedAt != nil {
		return ErrConflict
	}
	var current string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT current_attempt FROM operation_task_targets WHERE id=?", item.targetID).Scan(&current); err != nil {
		return ErrUnavailable
	}
	if current != item.attempt.ID {
		return ErrConflict
	}
	if item.attempt.StartedAt != nil {
		allowed, err := tx.runningPolicy(item)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrConflict
		}
		return nil
	}
	dispatch := &dispatch{nodeID: item.attempt.NodeID, targetID: item.targetID, taskID: item.taskID, ignoreAttempt: item.attempt.ID}
	var canceled bool
	var expires int64
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT policy_id,payload_encrypted,cancel_requested,expires_at FROM operation_tasks WHERE id=?", item.taskID).
		Scan(&dispatch.policyID, &dispatch.payload, &canceled, &expires); err != nil {
		return ErrUnavailable
	}
	if canceled || expires <= tx.now.UnixMilli() {
		return ErrConflict
	}
	local, digest, err := tx.capability(item.attempt.NodeID)
	if err != nil {
		return err
	}
	var expected string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT capability_digest FROM operation_attempts WHERE id=?", item.attempt.ID).Scan(&expected); err != nil {
		return ErrUnavailable
	}
	if digest != expected {
		return ErrConflict
	}
	return tx.validateDispatch(dispatch, local)
}
