package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

type AssignedNetworkTask struct {
	TaskID   string
	NodeID   string
	TaskType string
	Enabled  bool
	Assigned bool
}

func replaceTaskNodes(ctx context.Context, tx *sql.Tx, taskID string, allNodes bool, nodeIDs []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM network_task_nodes WHERE task_id = ?", taskID); err != nil {
		return fmt.Errorf("clear network task nodes: %w", err)
	}
	if allNodes {
		return nil
	}
	for _, nodeID := range nodeIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO network_task_nodes (task_id, node_id) VALUES (?, ?)", taskID, nodeID,
		); err != nil {
			return fmt.Errorf("assign network task node: %w", err)
		}
	}
	return nil
}

func (s *NetworkTaskStore) listTaskNodes(ctx context.Context, taskID string) ([]model.NetworkNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.name, n.alias
		FROM network_task_nodes t JOIN nodes n ON n.id = t.node_id
		WHERE t.task_id = ? ORDER BY n.name ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query network task nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	nodes := []model.NetworkNode{}
	for rows.Next() {
		var node model.NetworkNode
		if err := rows.Scan(&node.ID, &node.Name, &node.Alias); err != nil {
			return nil, fmt.Errorf("scan network task node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network task nodes: %w", err)
	}
	return nodes, nil
}

func (s *NetworkTaskStore) ListAssignments(
	ctx context.Context,
	nodeID string,
) ([]model.NetworkAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.type, t.target, t.ip_family,
		t.interval_seconds, t.timeout_milliseconds
		FROM network_tasks t JOIN nodes n ON n.id = ?
		WHERE t.enabled = 1 AND (t.all_nodes = 1 OR EXISTS (
			SELECT 1 FROM network_task_nodes a WHERE a.task_id = t.id AND a.node_id = n.id
		)) ORDER BY t.sort_order ASC, t.name ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query network assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	assignments := []model.NetworkAssignment{}
	for rows.Next() {
		var assignment model.NetworkAssignment
		if err := rows.Scan(&assignment.ID, &assignment.Name, &assignment.Type, &assignment.Target,
			&assignment.IPFamily, &assignment.IntervalSeconds, &assignment.TimeoutMilliseconds); err != nil {
			return nil, fmt.Errorf("scan network assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network assignments: %w", err)
	}
	return assignments, nil
}

func (s *NetworkTaskStore) GetAssignedTask(
	ctx context.Context,
	nodeID string,
	taskID string,
) (*AssignedNetworkTask, error) {
	var assigned AssignedNetworkTask
	err := s.db.QueryRowContext(ctx, `SELECT t.id, n.id, t.type, t.enabled,
		(t.all_nodes = 1 OR EXISTS (
			SELECT 1 FROM network_task_nodes a WHERE a.task_id = t.id AND a.node_id = n.id
		))
		FROM network_tasks t JOIN nodes n ON n.id = ? WHERE t.id = ?`, nodeID, taskID).
		Scan(&assigned.TaskID, &assigned.NodeID, &assigned.TaskType, &assigned.Enabled, &assigned.Assigned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query assigned network task: %w", err)
	}
	return &assigned, nil
}

func (s *NetworkTaskStore) ListEffectiveTaskNodes(
	ctx context.Context,
	taskID string,
	allNodes bool,
) ([]model.NetworkNode, error) {
	return s.listEffectiveTaskNodes(ctx, taskID, allNodes, false)
}

func (s *NetworkTaskStore) ListEffectivePublicTaskNodes(
	ctx context.Context,
	taskID string,
	allNodes bool,
) ([]model.NetworkNode, error) {
	return s.listEffectiveTaskNodes(ctx, taskID, allNodes, true)
}

func (s *NetworkTaskStore) listEffectiveTaskNodes(
	ctx context.Context,
	taskID string,
	allNodes bool,
	publicOnly bool,
) ([]model.NetworkNode, error) {
	query := `SELECT n.id, n.name, n.alias FROM nodes n`
	args := []any{}
	if !allNodes {
		query += ` JOIN network_task_nodes a ON a.node_id = n.id WHERE a.task_id = ?`
		args = append(args, taskID)
	}
	if publicOnly {
		if allNodes {
			query += ` WHERE n.is_public = 1`
		} else {
			query += ` AND n.is_public = 1`
		}
	}
	query += ` ORDER BY n.sort_order ASC, n.name ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query effective network task nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	nodes := []model.NetworkNode{}
	for rows.Next() {
		var node model.NetworkNode
		if err := rows.Scan(&node.ID, &node.Name, &node.Alias); err != nil {
			return nil, fmt.Errorf("scan effective network task node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective network task nodes: %w", err)
	}
	return nodes, nil
}

func (s *NetworkTaskStore) IsTaskAssignedToNode(
	ctx context.Context,
	taskID string,
	nodeID string,
) (bool, error) {
	var assigned bool
	err := s.db.QueryRowContext(ctx, `SELECT (t.all_nodes = 1 OR EXISTS (
		SELECT 1 FROM network_task_nodes a WHERE a.task_id = t.id AND a.node_id = n.id
	)) FROM network_tasks t JOIN nodes n ON n.id = ? WHERE t.id = ?`, nodeID, taskID).Scan(&assigned)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query network task node assignment: %w", err)
	}
	return assigned, nil
}
