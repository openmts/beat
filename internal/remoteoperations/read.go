package remoteoperations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type TaskSummary struct {
	RestoreRisk     bool            `json:"restore_risk"`
	State           string          `json:"state"`
	TargetCount     int             `json:"target_count"`
	SucceededCount  int             `json:"succeeded_count"`
	FailedCount     int             `json:"failed_count"`
	PendingCount    int             `json:"pending_count"`
	RunningCount    int             `json:"running_count"`
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	ActorID         string          `json:"actor_id"`
	CreatedAt       int64           `json:"created_at"`
	ExpiresAt       int64           `json:"expires_at"`
	CancelRequested bool            `json:"cancel_requested"`
	Targets         []TargetSummary `json:"targets,omitempty"`
}

type TargetSummary struct {
	RestoreRisk   bool   `json:"restore_risk"`
	AttemptNumber int    `json:"attempt_number"`
	StartedAt     *int64 `json:"started_at,omitempty"`
	CompletedAt   *int64 `json:"completed_at,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Reason        string `json:"reason,omitempty"`
	StdoutBytes   int    `json:"stdout_bytes"`
	StderrBytes   int    `json:"stderr_bytes"`
	ID            string `json:"id"`
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	State         string `json:"state"`
	AttemptID     string `json:"attempt_id"`
}

func (service *Service) ListTasks(ctx context.Context, actor Actor) ([]TaskSummary, error) {
	page, err := service.ListTaskPage(ctx, actor, TaskPageQuery{Limit: 200})
	return page.Items, err
}

func (service *Service) GetTask(ctx context.Context, actor Actor, id string) (TaskSummary, error) {
	var task TaskSummary
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		task, err = scanTaskSummary(tx.tx.QueryRowContext(ctx, taskSummarySelect+" WHERE t.id=? GROUP BY t.id", id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		task.Targets, err = tx.taskTargets(id)
		return err
	})
	return task, err
}

func (tx transaction) taskTargets(id string) (targets []TargetSummary, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT n.id,COALESCE(n.node_id,''),n.node_name,n.state,n.current_attempt,COALESCE(a.attempt_number,0),a.started_at,a.completed_at,
 CASE WHEN a.completed_at IS NULL THEN NULL ELSE json_extract(a.result_json,'$.exit_code') END,
 COALESCE(json_extract(a.result_json,'$.reason'),''),COALESCE(json_extract(a.result_json,'$.stdout_bytes'),0),COALESCE(json_extract(a.result_json,'$.stderr_bytes'),0),COALESCE(json_extract(t.policy_snapshot,'$.restore_risk'),0)
 FROM operation_task_targets n LEFT JOIN operation_attempts a ON a.id=n.current_attempt JOIN operation_tasks t ON t.id=n.task_id WHERE n.task_id=? ORDER BY n.node_name,n.id LIMIT 200`, id)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	targets = []TargetSummary{}
	for rows.Next() {
		var target TargetSummary
		if err := rows.Scan(&target.ID, &target.NodeID, &target.NodeName, &target.State, &target.AttemptID, &target.AttemptNumber, &target.StartedAt, &target.CompletedAt, &target.ExitCode, &target.Reason, &target.StdoutBytes, &target.StderrBytes, &target.RestoreRisk); err != nil {
			return nil, ErrUnavailable
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (service *Service) ListPolicies(ctx context.Context, actor Actor) ([]PolicyInput, error) {
	var policies []PolicyInput
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, false); err != nil {
			return err
		}
		var err error
		policies, err = tx.listPolicies()
		return err
	})
	return policies, err
}

func (tx transaction) listPolicies() (policies []PolicyInput, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT id,name,version,configuration_json,
		(SELECT json_group_array(node_id) FROM operation_policy_nodes WHERE policy_id=p.id),
		(SELECT json_group_array(group_id) FROM operation_policy_groups WHERE policy_id=p.id)
		FROM operation_policies p ORDER BY name,id LIMIT 200`)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	policies = []PolicyInput{}
	for rows.Next() {
		var policy PolicyInput
		var configuration, nodes, groups string
		if err := rows.Scan(&policy.ID, &policy.Name, &policy.Version, &configuration, &nodes, &groups); err != nil {
			return nil, ErrUnavailable
		}
		if json.Unmarshal([]byte(configuration), &policy.Policy) != nil || json.Unmarshal([]byte(nodes), &policy.NodeIDs) != nil || json.Unmarshal([]byte(groups), &policy.GroupIDs) != nil {
			return nil, ErrUnavailable
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}
