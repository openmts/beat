package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupAgentChannelService(t *testing.T) (*AgentChannelService, *store.NodeStore, *store.SSHKeyStore, string) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(context.Background(), "agent-node", "127.0.0.1", 30022)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	return NewAgentChannelService(store.NewAgentTaskStore(sqliteStore.DB)), nodes, store.NewSSHKeyStore(sqliteStore.DB), node.ID
}

func TestAgentChannelServiceEnqueueAndComplete(t *testing.T) {
	svc, _, _, nodeID := setupAgentChannelService(t)
	ctx := context.Background()

	task, err := svc.EnqueueAuthorizedKeyTask(ctx, nodeID, "ssh-ed25519 AAA")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if task.Status != model.AgentTaskStatusPending {
		t.Fatalf("status = %q, want pending", task.Status)
	}

	pending, err := svc.RegisterAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != task.ID {
		t.Fatalf("pending = %#v, want task %s", pending, task.ID)
	}

	if err := svc.CompleteAgentTask(ctx, task.ID, model.AgentTaskStatusDone, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	pending, err = svc.RegisterAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("register agent after complete: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after complete = %#v, want none", pending)
	}
}

func TestAgentChannelServiceCompleteFailed(t *testing.T) {
	svc, _, _, nodeID := setupAgentChannelService(t)
	ctx := context.Background()

	task, err := svc.EnqueueAuthorizedKeyTask(ctx, nodeID, "ssh-ed25519 AAA")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := svc.CompleteAgentTask(ctx, task.ID, model.AgentTaskStatusFailed, "boom"); err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	pending, err := svc.RegisterAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after failure = %#v, want none", pending)
	}
}
