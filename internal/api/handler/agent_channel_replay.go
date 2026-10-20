package handler

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
)

type agentTaskReplayer interface {
	ReplayAgentTasks(context.Context, string, func(model.AgentTask) error) error
}

func (h *AgentChannelHandler) replayAgentTasks(ctx context.Context, nodeID string, connection *agentWebSocketConn) error {
	if replayer, ok := h.operations.(agentTaskReplayer); ok {
		return replayer.ReplayAgentTasks(ctx, nodeID, func(task model.AgentTask) error { return connection.WriteTask(ctx, task) })
	}
	pending, err := h.operations.RegisterAgent(ctx, nodeID)
	if err != nil {
		return err
	}
	if len(pending) > maximumFallbackAgentReplay {
		return errors.New("agent replay exceeds its bounded batch")
	}
	for _, task := range pending {
		if err := connection.WriteTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

const maximumFallbackAgentReplay = 1024
