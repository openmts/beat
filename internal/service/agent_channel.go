package service

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// AgentChannelService implements the operations the agent channel handler
// needs, backed by the persistent agent task store.
type AgentChannelService struct {
	tasks *store.AgentTaskStore
}

func NewAgentChannelService(tasks *store.AgentTaskStore) *AgentChannelService {
	return &AgentChannelService{tasks: tasks}
}

// EnqueueAuthorizedKeyTask queues a key distribution task for a node.
func (s *AgentChannelService) EnqueueAuthorizedKeyTask(
	ctx context.Context,
	nodeID string,
	publicKey string,
) (*model.AgentTask, error) {
	payload, err := model.EncodeAuthorizedKeyPayload(publicKey)
	if err != nil {
		return nil, fmt.Errorf("encode authorized key task: %w", err)
	}
	return s.tasks.CreateTask(ctx, nodeID, model.AgentTaskAuthorizedKey, payload)
}

// RegisterAgent marks an agent connection and returns its pending tasks so the
// channel can replay them after a reconnect.
func (s *AgentChannelService) RegisterAgent(ctx context.Context, nodeID string) ([]model.AgentTask, error) {
	return s.tasks.GetPendingTasks(ctx, nodeID)
}

// CompleteAgentTask records a task result.
func (s *AgentChannelService) CompleteAgentTask(
	ctx context.Context,
	taskID string,
	status string,
	message string,
) error {
	if message != "" {
		return s.tasks.CompleteTask(ctx, taskID, status, message)
	}
	return s.tasks.CompleteTask(ctx, taskID, status, "")
}
