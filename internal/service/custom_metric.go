package service

import (
	"context"
	"fmt"
	"log/slog"
)

// CustomMetricDispatchService enqueues custom metric registration tasks and
// pushes them to connected agents.
type CustomMetricDispatchService struct {
	channel *AgentChannelService
	hub     *AgentHub
}

func NewCustomMetricDispatchService(channel *AgentChannelService, hub *AgentHub) *CustomMetricDispatchService {
	return &CustomMetricDispatchService{channel: channel, hub: hub}
}

// RegisterNodeCustomMetrics enqueues a registration task for a node and pushes
// it when the agent is online. Offline agents receive the pending task after
// reconnecting.
func (s *CustomMetricDispatchService) RegisterNodeCustomMetrics(ctx context.Context, nodeID string) error {
	task, err := s.channel.EnqueueCustomMetricRegisterTask(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("enqueue custom metric register: %w", err)
	}
	if s.hub != nil && !s.hub.PushTask(ctx, *task) {
		slog.DebugContext(ctx, "agent offline, custom metric register will replay on reconnect",
			"node_id", nodeID, "task_id", task.ID)
	}
	return nil
}
