package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/model"
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
	return s.DispatchCustomMetricTasks(ctx, []model.AgentTask{*task})
}

// 快照已与元数据一起持久化，客户端断开不会取消投递；失败连接由Hub关闭以触发重放。
func (s *CustomMetricDispatchService) DispatchCustomMetricTasks(ctx context.Context, tasks []model.AgentTask) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, task := range tasks {
		if s.hub != nil && !s.hub.PushTask(ctx, task) {
			slog.DebugContext(ctx, "custom metric snapshot pending replay", "node_id", task.NodeID, "task_id", task.ID)
		}
	}
	return nil
}
