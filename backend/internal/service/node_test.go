package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupTestNodeService(t *testing.T) *NodeService {
	t.Helper()

	sqliteStore, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	if _, err := sqliteStore.DB.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "beat-mts-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	mtsStore, err := store.NewMTSStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create mts store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })

	nodeStore := store.NewNodeStore(sqliteStore.DB)
	svc := NewNodeService(nodeStore, mtsStore)
	return svc
}

func TestNewNodeService(t *testing.T) {
	svc := setupTestNodeService(t)
	if svc == nil {
		t.Fatal("NewNodeService returned nil")
	}
}

func TestGetNodeSummaryEmpty(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	summary, err := svc.GetNodeSummary(ctx, "nonexistent-node")
	if err != nil {
		t.Fatalf("GetNodeSummary error: %v", err)
	}

	if summary == nil {
		t.Fatal("summary should not be nil")
	}
	if summary.CPU != 0 {
		t.Errorf("expected CPU 0, got %f", summary.CPU)
	}
	if summary.Memory != 0 {
		t.Errorf("expected Memory 0, got %f", summary.Memory)
	}
	if summary.DiskRead != 0 {
		t.Errorf("expected DiskRead 0, got %f", summary.DiskRead)
	}
	if summary.DiskWrite != 0 {
		t.Errorf("expected DiskWrite 0, got %f", summary.DiskWrite)
	}
	if summary.NetRecv != 0 {
		t.Errorf("expected NetRecv 0, got %f", summary.NetRecv)
	}
	if summary.NetSent != 0 {
		t.Errorf("expected NetSent 0, got %f", summary.NetSent)
	}
}

func TestRegisterNode(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	node, err := svc.RegisterNode(ctx, "test-host", 22, nil)
	if err != nil {
		t.Fatalf("RegisterNode error: %v", err)
	}

	if node == nil {
		t.Fatal("RegisterNode returned nil")
	}
	if node.ID == "" {
		t.Error("node ID should not be empty")
	}
	if node.Name != "test-host" {
		t.Errorf("node name = %q, want %q", node.Name, "test-host")
	}
	if node.Host != "test-host" {
		t.Errorf("node host = %q, want %q", node.Host, "test-host")
	}
	if node.Port != 22 {
		t.Errorf("node port = %d, want %d", node.Port, 22)
	}
	if node.Status != model.NodeStatusOnline {
		t.Errorf("node status = %q, want %q", node.Status, model.NodeStatusOnline)
	}
}

func TestGetNodeSummary(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	metrics := &model.NodeMetrics{
		CPU:         75.0,
		CPUUsed:     6.0,
		CPUTotal:    8.0,
		Memory:      85.0,
		MemoryUsed:  680.0,
		MemoryTotal: 800.0,
		DiskUsed:    70.0,
		DiskTotal:   120.0,
		DiskRead:    15.0,
		DiskWrite:   25.0,
		NetRecv:     150.0,
		NetSent:     250.0,
	}

	node, err := svc.RegisterNode(ctx, "summary-node", 22, metrics)
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	if err := svc.mtsStore.Flush(ctx); err != nil {
		t.Fatalf("flush metrics: %v", err)
	}

	summary, err := svc.GetNodeSummary(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNodeSummary error: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}
	if summary.CPUUsed != 6 || summary.CPUTotal != 8 ||
		summary.MemoryUsed != 680 || summary.MemoryTotal != 800 ||
		summary.DiskUsed != 70 || summary.DiskTotal != 120 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestGetNodeMetrics(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	now := time.Now().UTC()
	metrics := &model.NodeMetrics{
		CPU:       30.0,
		Memory:    40.0,
		DiskRead:  5.0,
		DiskWrite: 10.0,
		NetRecv:   50.0,
		NetSent:   100.0,
	}

	_, _ = svc.RegisterNode(ctx, "metrics-node", 22, metrics)

	result, err := svc.GetNodeMetrics(ctx, "metrics-node", []string{"cpu", "memory"}, now.Add(-1*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(result))
	}
}

func TestGetNodeMetrics_Empty(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	result, err := svc.GetNodeMetrics(ctx, "nonexistent", []string{"cpu"}, now.Add(-1*time.Hour), now)
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result["cpu"]) != 0 {
		t.Fatalf("expected 0 cpu points, got %d", len(result["cpu"]))
	}
}

func TestGetNodeMetrics_NoMetrics(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	result, err := svc.GetNodeMetrics(ctx, "nonexistent", []string{}, now.Add(-1*time.Hour), now)
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 metrics, got %d", len(result))
	}
}

func TestGetNodeSummary_WithMetrics(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	metrics := &model.NodeMetrics{
		CPU:       33.3,
		Memory:    44.4,
		DiskRead:  55.5,
		DiskWrite: 66.6,
		NetRecv:   77.7,
		NetSent:   88.8,
	}

	node, _ := svc.RegisterNode(ctx, "summary-node-2", 22, metrics)

	summary, err := svc.GetNodeSummary(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNodeSummary error: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}
}

func TestGetNodeMetrics_MultipleMetrics(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	metrics := &model.NodeMetrics{
		CPU:       10.0,
		Memory:    20.0,
		DiskRead:  30.0,
		DiskWrite: 40.0,
		NetRecv:   50.0,
		NetSent:   60.0,
	}

	_, _ = svc.RegisterNode(ctx, "multi-metrics", 22, metrics)

	allMetrics := []string{
		"cpu", "cpu_used", "cpu_total", "memory", "memory_used", "memory_total",
		"disk_used", "disk_total",
		"disk_read", "disk_write", "net_recv", "net_sent",
	}
	result, err := svc.GetNodeMetrics(ctx, "multi-metrics", allMetrics, now.Add(-1*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result) != 12 {
		t.Fatalf("expected 12 metrics, got %d", len(result))
	}
}

func TestGetNodeMetrics_TimeRange(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := context.Background()

	now := time.Now().UTC()
	metrics := &model.NodeMetrics{
		CPU:       90.0,
		Memory:    80.0,
		DiskRead:  70.0,
		DiskWrite: 60.0,
		NetRecv:   50.0,
		NetSent:   40.0,
	}

	_, _ = svc.RegisterNode(ctx, "time-range-node", 22, metrics)

	result, err := svc.GetNodeMetrics(ctx, "time-range-node", []string{"cpu"}, now.Add(1*time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result["cpu"]) != 0 {
		t.Fatalf("expected 0 cpu points in future range, got %d", len(result["cpu"]))
	}
}
