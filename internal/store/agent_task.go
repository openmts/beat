package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type AgentTaskStore struct {
	db *sql.DB
}

const agentTaskColumns = `id, node_id, type, payload, status, output, failure, created_at, attempted_at, completed_at`

func NewAgentTaskStore(db *sql.DB) *AgentTaskStore {
	return &AgentTaskStore{db: db}
}

func (s *AgentTaskStore) CreateTask(
	ctx context.Context,
	nodeID string,
	taskType string,
	payload string,
) (*model.AgentTask, error) {
	now := model.NowUTC()
	task := &model.AgentTask{
		ID:        uuid.New().String(),
		NodeID:    nodeID,
		Type:      taskType,
		Payload:   payload,
		Status:    model.AgentTaskStatusPending,
		CreatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO agent_tasks (id, node_id, type, payload, status, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		task.ID, task.NodeID, task.Type, task.Payload, task.Status, task.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting agent task: %w", err)
	}
	return task, nil
}

func (s *AgentTaskStore) GetPendingTasks(ctx context.Context, nodeID string) ([]model.AgentTask, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+agentTaskColumns+" FROM agent_tasks WHERE node_id = ? AND status = ? ORDER BY created_at ASC",
		nodeID, model.AgentTaskStatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pending agent tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []model.AgentTask
	for rows.Next() {
		var task model.AgentTask
		if err := rows.Scan(
			&task.ID, &task.NodeID, &task.Type, &task.Payload, &task.Status, &task.Output, &task.Failure,
			&task.CreatedAt, &task.AttemptedAt, &task.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating agent tasks: %w", err)
	}
	return tasks, nil
}

func (s *AgentTaskStore) CompleteTask(
	ctx context.Context,
	taskID string,
	status string,
	failure string,
	output string,
) error {
	if status != model.AgentTaskStatusDone && status != model.AgentTaskStatusFailed {
		return fmt.Errorf("invalid agent task status %q", status)
	}
	now := model.NowUTC()
	result, err := s.db.ExecContext(ctx,
		"UPDATE agent_tasks SET status = ?, failure = ?, output = ?, attempted_at = COALESCE(attempted_at, ?), completed_at = ? WHERE id = ? AND status = ?",
		status, model.TruncateAgentTaskFailure(failure), model.TruncateAgentTaskOutput(output), now, now, taskID, model.AgentTaskStatusPending,
	)
	if err != nil {
		return fmt.Errorf("completing agent task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading agent task update count: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("agent task %q is not pending", taskID)
	}
	return nil
}

// GetTask 读取单个任务及其有界诊断字段，供管理端查询试运行结果。
func (s *AgentTaskStore) GetTask(ctx context.Context, taskID string) (*model.AgentTask, error) {
	var task model.AgentTask
	err := s.db.QueryRowContext(ctx, "SELECT "+agentTaskColumns+" FROM agent_tasks WHERE id = ?", taskID).Scan(
		&task.ID, &task.NodeID, &task.Type, &task.Payload, &task.Status, &task.Output, &task.Failure,
		&task.CreatedAt, &task.AttemptedAt, &task.CompletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading agent task: %w", err)
	}
	return &task, nil
}
