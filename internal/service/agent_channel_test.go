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

func TestAgentChannelServiceEnqueueCustomMetricRegister(t *testing.T) {
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	ctx := context.Background()
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(ctx, "agent-node", "127.0.0.1", 30022)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	customs := store.NewCustomMetricStore(sqliteStore.DB)
	metric, err := customs.Create(ctx, model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	if err := customs.SetNodeBindings(ctx, node.ID, []string{metric.ID}); err != nil {
		t.Fatalf("set bindings: %v", err)
	}

	svc := NewAgentChannelService(store.NewAgentTaskStore(sqliteStore.DB)).WithCustomMetrics(customs)
	task, err := svc.EnqueueCustomMetricRegisterTask(ctx, node.ID)
	if err != nil {
		t.Fatalf("enqueue register: %v", err)
	}
	if task.Type != model.AgentTaskCustomMetricRegister {
		t.Fatalf("type = %q", task.Type)
	}
	payload, err := model.DecodeCustomMetricRegisterPayload(task.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Metrics) != 1 || payload.Metrics[0].Name != "latency" {
		t.Fatalf("payload metrics = %#v", payload.Metrics)
	}
}

func TestAgentChannelServiceEnqueueCustomMetricRegisterWithoutStore(t *testing.T) {
	svc, _, _, nodeID := setupAgentChannelService(t)
	ctx := context.Background()
	if _, err := svc.EnqueueCustomMetricRegisterTask(ctx, nodeID); err == nil {
		t.Fatal("expected error when custom metric store is not configured")
	}
}

func TestAgentChannelServiceRefreshCustomMetrics(t *testing.T) {
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	ctx := context.Background()
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(ctx, "agent-node", "127.0.0.1", 30022)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	customs := store.NewCustomMetricStore(sqliteStore.DB)
	metric, err := customs.Create(ctx, model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	if err := customs.SetMetricNodes(ctx, metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("set metric nodes: %v", err)
	}
	svc := NewAgentChannelService(store.NewAgentTaskStore(sqliteStore.DB)).WithCustomMetrics(customs)
	task, err := svc.RefreshCustomMetrics(ctx, node.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if task == nil {
		t.Fatal("task should not be nil")
	}
	payload, err := model.DecodeCustomMetricRegisterPayload(task.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Metrics) != 1 || payload.Metrics[0].Name != "latency" {
		t.Fatalf("payload = %#v", payload.Metrics)
	}
}

func TestAgentChannelServiceRefreshCustomMetricsWithoutStore(t *testing.T) {
	svc, _, _, nodeID := setupAgentChannelService(t)
	ctx := context.Background()
	task, err := svc.RefreshCustomMetrics(ctx, nodeID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil without store", task)
	}
}

func TestCustomMetricDispatchService(t *testing.T) {
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	ctx := context.Background()
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(ctx, "agent-node", "127.0.0.1", 30022)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	customs := store.NewCustomMetricStore(sqliteStore.DB)
	metric, err := customs.Create(ctx, model.CustomMetric{
		Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	if err := customs.SetMetricNodes(ctx, metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("set metric nodes: %v", err)
	}
	channel := NewAgentChannelService(store.NewAgentTaskStore(sqliteStore.DB)).WithCustomMetrics(customs)
	dispatcher := NewCustomMetricDispatchService(channel, NewAgentHub())
	if err := dispatcher.RegisterNodeCustomMetrics(ctx, node.ID); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	pending, err := channel.tasks.GetPendingTasks(ctx, node.ID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
}
