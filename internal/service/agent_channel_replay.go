package service

import (
	"context"

	"github.com/beat/backend/internal/model"
)

func (s *AgentChannelService) ReplayAgentTasks(ctx context.Context, nodeID string, consume func(model.AgentTask) error) error {
	return s.tasks.ReplayPendingTasks(ctx, nodeID, consume)
}
