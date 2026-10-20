package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAgentTaskStoreLifecycle(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "node-one", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	stores := NewAgentTaskStore(s.DB)

	task, err := stores.CreateTask(ctx, node.ID, model.AgentTaskAuthorizedKey, `{"public_key":"ssh-ed25519 AAA"}`)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.ID == "" || task.Status != model.AgentTaskStatusPending {
		t.Fatalf("task = %#v, want id set and pending", task)
	}

	got, err := stores.GetPendingTasks(ctx, node.ID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if len(got) != 1 || got[0].ID != task.ID {
		t.Fatalf("pending = %#v, want task %s", got, task.ID)
	}

	if err := stores.CompleteTask(ctx, task.ID, model.AgentTaskStatusDone, "", ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	after, err := stores.GetPendingTasks(ctx, node.ID)
	if err != nil {
		t.Fatalf("get pending after complete: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("pending after complete = %#v, want none", after)
	}

	if err := stores.CompleteTask(ctx, task.ID, model.AgentTaskStatusDone, "", ""); err == nil {
		t.Fatal("completing a non-pending task should fail")
	}
}

func TestAgentTaskStorePendingScopedToNode(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	nodeA, err := nodeStore.UpsertNode(ctx, "node-a", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node-a: %v", err)
	}
	nodeB, err := nodeStore.UpsertNode(ctx, "node-b", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create node-b: %v", err)
	}
	stores := NewAgentTaskStore(s.DB)

	if _, err := stores.CreateTask(ctx, nodeA.ID, model.AgentTaskAuthorizedKey, `{"public_key":"aaa"}`); err != nil {
		t.Fatalf("create task for node-a: %v", err)
	}
	if _, err := stores.CreateTask(ctx, nodeB.ID, model.AgentTaskAuthorizedKey, `{"public_key":"bbb"}`); err != nil {
		t.Fatalf("create task for node-b: %v", err)
	}

	got, err := stores.GetPendingTasks(ctx, nodeA.ID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if len(got) != 1 || got[0].NodeID != nodeA.ID {
		t.Fatalf("pending = %#v, want only node-a task", got)
	}
}

func TestAgentTaskStoreInvalidStatus(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "node-one", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	stores := NewAgentTaskStore(s.DB)

	task, err := stores.CreateTask(ctx, node.ID, model.AgentTaskAuthorizedKey, `{"public_key":"aaa"}`)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := stores.CompleteTask(ctx, task.ID, "bogus", "", ""); err == nil {
		t.Fatal("completing task with invalid status should fail")
	}
	if err := stores.CompleteTask(ctx, task.ID, model.AgentTaskStatusFailed, "boom", ""); err != nil {
		t.Fatalf("fail task: %v", err)
	}
}
