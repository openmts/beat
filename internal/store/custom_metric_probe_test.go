package store

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func createProbeNode(t *testing.T, sqlite *SQLiteStore, name string) *model.Node {
	t.Helper()
	node, err := NewNodeStore(sqlite.DB).UpsertNode(context.Background(), name, "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node %s: %v", name, err)
	}
	return node
}

// bindProbeMetric 建一个自定义指标并把它绑定到新节点上，返回节点与指标。
func bindProbeMetric(t *testing.T, sqlite *SQLiteStore, metrics *CustomMetricStore, enabled bool) (*model.Node, *model.CustomMetric) {
	t.Helper()
	node := createProbeNode(t, sqlite, "probe-node")
	metric, err := metrics.Create(context.Background(), model.CustomMetric{
		Name: "probe_latency", ShellCommand: "echo 5", IntervalSeconds: 15, Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	if err := metrics.SetMetricNodes(context.Background(), metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("bind custom metric: %v", err)
	}
	return node, metric
}

func TestQueueCustomMetricProbeCreatesPendingTask(t *testing.T) {
	ctx := context.Background()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	node, metric := bindProbeMetric(t, sqlite, metrics, true)

	task, err := metrics.QueueCustomMetricProbe(ctx, metric.ID, node.ID)
	if err != nil {
		t.Fatalf("queue probe: %v", err)
	}
	if task.Type != model.AgentTaskCustomMetricProbe || task.Status != model.AgentTaskStatusPending {
		t.Fatalf("task = %#v", task)
	}
	if task.NodeID != node.ID || task.ID == "" {
		t.Fatalf("task identity = %#v", task)
	}
	payload, err := model.DecodeCustomMetricProbePayload(task.Payload)
	if err != nil || payload.MetricID != metric.ID || payload.ShellCommand != metric.ShellCommand {
		t.Fatalf("payload = %#v err = %v", payload, err)
	}

	read, err := metrics.CustomMetricProbe(ctx, metric.ID, task.ID)
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if read == nil || read.ID != task.ID || read.Status != model.AgentTaskStatusPending {
		t.Fatalf("read = %#v", read)
	}
}

func TestQueueCustomMetricProbeRejectsInvalidRequests(t *testing.T) {
	ctx := context.Background()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	node, metric := bindProbeMetric(t, sqlite, metrics, true)
	unbound := createProbeNode(t, sqlite, "unbound-node")

	if _, err := metrics.QueueCustomMetricProbe(ctx, "", node.ID); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatalf("empty metric error = %v, want ErrCustomMetricInvalid", err)
	}
	if _, err := metrics.QueueCustomMetricProbe(ctx, metric.ID, ""); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatalf("empty node error = %v, want ErrCustomMetricInvalid", err)
	}
	if _, err := metrics.QueueCustomMetricProbe(ctx, "missing-metric", node.ID); !errors.Is(err, ErrCustomMetricProbeNotFound) {
		t.Fatalf("unknown metric error = %v, want ErrCustomMetricProbeNotFound", err)
	}
	if _, err := metrics.QueueCustomMetricProbe(ctx, metric.ID, unbound.ID); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatalf("unbound node error = %v, want ErrCustomMetricInvalid", err)
	}
}

func TestQueueCustomMetricProbeRejectsDisabledMetric(t *testing.T) {
	ctx := context.Background()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	node, metric := bindProbeMetric(t, sqlite, metrics, false)

	if _, err := metrics.QueueCustomMetricProbe(ctx, metric.ID, node.ID); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatalf("disabled metric error = %v, want ErrCustomMetricInvalid", err)
	}
}

func TestCustomMetricProbeRejectsForeignTasks(t *testing.T) {
	ctx := context.Background()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	tasks := NewAgentTaskStore(sqlite.DB)
	node, metric := bindProbeMetric(t, sqlite, metrics, true)

	if _, err := metrics.CustomMetricProbe(ctx, metric.ID, "missing-task"); !errors.Is(err, ErrCustomMetricProbeNotFound) {
		t.Fatalf("missing task error = %v, want ErrCustomMetricProbeNotFound", err)
	}

	foreign, err := tasks.CreateTask(ctx, node.ID, model.AgentTaskAuthorizedKey, `{"public_key":"ssh-ed25519 AAAA"}`)
	if err != nil {
		t.Fatalf("create foreign task: %v", err)
	}
	if _, err := metrics.CustomMetricProbe(ctx, metric.ID, foreign.ID); !errors.Is(err, ErrCustomMetricProbeNotFound) {
		t.Fatalf("foreign type error = %v, want ErrCustomMetricProbeNotFound", err)
	}

	broken, err := tasks.CreateTask(ctx, node.ID, model.AgentTaskCustomMetricProbe, "not-json")
	if err != nil {
		t.Fatalf("create broken payload task: %v", err)
	}
	if _, err := metrics.CustomMetricProbe(ctx, metric.ID, broken.ID); !errors.Is(err, ErrCustomMetricProbeNotFound) {
		t.Fatalf("broken payload error = %v, want ErrCustomMetricProbeNotFound", err)
	}
}

func TestCustomMetricProbeRejectsOtherMetricTask(t *testing.T) {
	ctx := context.Background()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	node, metric := bindProbeMetric(t, sqlite, metrics, true)

	task, err := metrics.QueueCustomMetricProbe(ctx, metric.ID, node.ID)
	if err != nil {
		t.Fatalf("queue probe: %v", err)
	}
	if _, err := metrics.CustomMetricProbe(ctx, "other-metric", task.ID); !errors.Is(err, ErrCustomMetricProbeNotFound) {
		t.Fatalf("metric mismatch error = %v, want ErrCustomMetricProbeNotFound", err)
	}
	if _, err := metrics.CustomMetricProbe(ctx, metric.ID, task.ID); err != nil {
		t.Fatalf("owning metric read: %v", err)
	}
}
