package store

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

const agentTaskReplayPageSize = 32

const maximumAgentTaskPayloadBytes = 1 << 20

type agentTaskCursor struct {
	id      string
	created time.Time
}

func (s *AgentTaskStore) ReplayPendingTasks(ctx context.Context, nodeID string, consume func(model.AgentTask) error) error {
	if consume == nil {
		return errors.New("agent task consumer is required")
	}
	cursor := agentTaskCursor{created: time.Unix(0, 0).UTC()}
	for {
		page, err := s.pendingTaskPage(ctx, nodeID, cursor)
		if err != nil {
			return err
		}
		for _, task := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := consume(task); err != nil {
				return err
			}
			cursor = agentTaskCursor{id: task.ID, created: task.CreatedAt}
		}
		if len(page) < agentTaskReplayPageSize {
			return nil
		}
	}
}

func (s *AgentTaskStore) pendingTaskPage(ctx context.Context, nodeID string, cursor agentTaskCursor) ([]model.AgentTask, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+agentTaskColumns+` FROM agent_tasks
		WHERE node_id=? AND status=? AND (created_at>? OR (created_at=? AND id>?))
		ORDER BY created_at ASC,id ASC LIMIT ?`, nodeID, model.AgentTaskStatusPending, cursor.created, cursor.created, cursor.id, agentTaskReplayPageSize)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	page := make([]model.AgentTask, 0, agentTaskReplayPageSize)
	for rows.Next() {
		var task model.AgentTask
		if err := rows.Scan(&task.ID, &task.NodeID, &task.Type, &task.Payload, &task.Status, &task.Output, &task.Failure, &task.CreatedAt, &task.AttemptedAt, &task.CompletedAt); err != nil {
			return nil, err
		}
		if len(task.Payload) > maximumAgentTaskPayloadBytes {
			return nil, errors.New("agent task payload exceeds limit")
		}
		page = append(page, task)
	}
	return page, rows.Err()
}
