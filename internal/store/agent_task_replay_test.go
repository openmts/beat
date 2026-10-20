package store

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func pendingReplayFixture(t *testing.T, count int) (*AgentTaskStore, string) {
	t.Helper()
	sqlite := setupTestDB(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "replay-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	tasks := NewAgentTaskStore(sqlite.DB)
	for range count {
		if _, err := tasks.CreateTask(t.Context(), node.ID, model.AgentTaskAuthorizedKey, `{"public_key":"ssh-ed25519 test"}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlite.DB.Exec("UPDATE agent_tasks SET created_at=?", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return tasks, node.ID
}

func TestTaskReplayPagesSameTimestampWithoutSkippingConcurrentAcknowledgements(t *testing.T) {
	tasks, nodeID := pendingReplayFixture(t, 2*agentTaskReplayPageSize+1)
	var delivered []string
	if err := tasks.ReplayPendingTasks(t.Context(), nodeID, func(task model.AgentTask) error {
		delivered = append(delivered, task.ID)
		return tasks.CompleteTask(t.Context(), task.ID, model.AgentTaskStatusDone, "", "")
	}); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2*agentTaskReplayPageSize+1 || !sort.StringsAreSorted(delivered) {
		t.Fatal("keyset replay skipped or reordered same-time tasks")
	}
	pending, err := tasks.GetPendingTasks(t.Context(), nodeID)
	if err != nil || len(pending) != 0 {
		t.Fatal("live acknowledgement blocked replay", err)
	}
}

func TestTaskReplayPropagatesCancellationConsumerAndStorageErrors(t *testing.T) {
	tasks, nodeID := pendingReplayFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	if err := tasks.ReplayPendingTasks(ctx, nodeID, func(model.AgentTask) error { calls++; cancel(); return nil }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled replay kept delivering", err)
	}
	want := errors.New("consumer stopped")
	if err := tasks.ReplayPendingTasks(t.Context(), nodeID, func(model.AgentTask) error { return want }); !errors.Is(err, want) {
		t.Fatal("consumer failure swallowed")
	}
	if err := tasks.ReplayPendingTasks(t.Context(), nodeID, nil); err == nil {
		t.Fatal("nil consumer admitted")
	}
	if err := tasks.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tasks.ReplayPendingTasks(t.Context(), nodeID, func(model.AgentTask) error { return nil }); err == nil {
		t.Fatal("closed task storage became empty replay")
	}
}
