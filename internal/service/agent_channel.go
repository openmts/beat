package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// AgentChannelService implements the operations the agent channel handler
// needs, backed by the persistent agent task store.
type AgentChannelService struct {
	tasks   *store.AgentTaskStore
	customs *store.CustomMetricStore
}

func NewAgentChannelService(tasks *store.AgentTaskStore) *AgentChannelService {
	return &AgentChannelService{tasks: tasks}
}

// WithCustomMetrics attaches the custom metric store for registration tasks.
func (s *AgentChannelService) WithCustomMetrics(customs *store.CustomMetricStore) *AgentChannelService {
	s.customs = customs
	return s
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

// EnqueueCustomMetricRegisterTask queues a task that instructs the agent to
// register the full set of enabled custom metrics bound to a node.
func (s *AgentChannelService) EnqueueCustomMetricRegisterTask(ctx context.Context, nodeID string) (*model.AgentTask, error) {
	if s.customs == nil {
		return nil, errors.New("custom metric store is not configured")
	}
	return s.RefreshCustomMetrics(ctx, nodeID)
}

// RegisterAgent marks an agent connection and returns its pending tasks so the
// channel can replay them after a reconnect.
func (s *AgentChannelService) RegisterAgent(ctx context.Context, nodeID string) ([]model.AgentTask, error) {
	return s.tasks.GetPendingTasks(ctx, nodeID)
}

// RefreshCustomMetrics enqueues a registration task for a node so its agent
// rebuilds the local custom metric registry.
func (s *AgentChannelService) RefreshCustomMetrics(ctx context.Context, nodeID string) (*model.AgentTask, error) {
	if s.customs == nil {
		return nil, nil
	}
	return s.customs.QueueNodeCustomMetrics(ctx, nodeID)
}

// CompleteAgentTask 记录一次任务终态。成功与否由 result 决定，诊断字段一并落库。
func (s *AgentChannelService) CompleteAgentTask(ctx context.Context, result model.AgentTaskResult) error {
	status := model.AgentTaskStatusFailed
	if result.Success {
		status = model.AgentTaskStatusDone
	}
	if identity, ok := middleware.AgentIdentity(ctx); ok {
		return s.tasks.CompleteTaskForNode(ctx, store.AgentTaskCompletion{
			NodeID: identity.NodeID, TaskID: result.TaskID, Status: status,
			Output: result.Output, Failure: result.Error,
		})
	}
	return s.tasks.CompleteTask(ctx, result.TaskID, status, result.Error, result.Output)
}
