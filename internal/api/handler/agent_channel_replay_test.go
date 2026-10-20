package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

func TestAgentReplayUsesPagedProductionStoreAndPreservesCancellation(t *testing.T) {
	sqlite := setupTestDB(t)
	nodes := store.NewNodeStore(sqlite.DB)
	node, err := nodes.UpsertNode(t.Context(), "streamed-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	tasks := store.NewAgentTaskStore(sqlite.DB)
	if _, err := tasks.CreateTask(t.Context(), node.ID, model.AgentTaskAuthorizedKey, `{"public_key":"ssh-ed25519 test"}`); err != nil {
		t.Fatal(err)
	}
	handler := NewAgentChannelHandler(service.NewAgentChannelService(tasks), service.NewAgentHub())
	connection := &agentWebSocketConn{writeCh: make(chan []byte, 1)}
	if err := handler.replayAgentTasks(t.Context(), node.ID, connection); err != nil {
		t.Fatal(err)
	}
	if len(connection.writeCh) != 1 {
		t.Fatal("production replay did not stream the task")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := handler.replayAgentTasks(ctx, node.ID, connection); !errors.Is(err, context.Canceled) {
		t.Fatal("production replay ignored cancellation", err)
	}
}

func TestAgentReplayBoundsFallbackAndCancelsBlockedDelivery(t *testing.T) {
	ops := &fakeChannelOps{pending: make([]model.AgentTask, maximumFallbackAgentReplay+1)}
	handler := NewAgentChannelHandler(ops, service.NewAgentHub())
	connection := &agentWebSocketConn{}
	if err := handler.replayAgentTasks(t.Context(), "node", connection); err == nil {
		t.Fatal("fallback replay was unbounded")
	}
	ops.pending = []model.AgentTask{{ID: "task", Type: model.AgentTaskAuthorizedKey}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := handler.replayAgentTasks(ctx, "node", connection); !errors.Is(err, context.Canceled) {
		t.Fatal("blocked replay failed to cancel", err)
	}
}
