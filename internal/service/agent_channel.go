package service

import (
	"context"
	"errors"
	"fmt"

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
	metrics, err := s.customs.ListEnabledForNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list enabled custom metrics: %w", err)
	}
	payload, err := model.EncodeCustomMetricRegisterPayload(metrics)
	if err != nil {
		return nil, fmt.Errorf("encode custom metric register task: %w", err)
	}
	return s.tasks.CreateTask(ctx, nodeID, model.AgentTaskCustomMetricRegister, payload)
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
