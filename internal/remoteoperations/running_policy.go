package remoteoperations

import "time"

func (tx transaction) runningPolicy(item storedAttempt) (bool, error) {
	var policyID, kind, current string
	var timeout, output int
	var canceled bool
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT t.policy_id,t.kind,t.timeout_seconds,t.output_bytes,t.cancel_requested,n.current_attempt FROM operation_tasks t JOIN operation_task_targets n ON n.task_id=t.id WHERE t.id=? AND n.id=?", item.taskID, item.targetID).
		Scan(&policyID, &kind, &timeout, &output, &canceled, &current)
	if err != nil {
		return false, ErrUnavailable
	}
	if current != item.attempt.ID || canceled || item.attempt.StartedAt == nil || !tx.now.Before(item.attempt.StartedAt.Add(time.Duration(timeout)*time.Second)) {
		return false, nil
	}
	policy, err := tx.policy(policyID)
	if err == ErrForbidden || err == ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := tx.scope(policyID, item.attempt.NodeID); err == ErrForbidden {
		return false, nil
	} else if err != nil {
		return false, err
	}
	limits := Limits{Kind: kind, Targets: 1, TimeoutSeconds: timeout, OutputBytes: output}
	return Authorize(policy, policy, limits) == nil, nil
}
