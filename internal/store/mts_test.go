package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func setupTestMTS(t *testing.T) *MTSStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mts_test")

	s, err := NewMTSStore(path)
	if err != nil {
		t.Fatalf("new mts store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestNewMTSStore(t *testing.T) {
	s := setupTestMTS(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if s.engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestNewMTSStore_Error(t *testing.T) {
	_, err := NewMTSStore("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestWriteMetricAndQueryMetrics(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()

	ts1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Minute)
	ts3 := ts1.Add(2 * time.Minute)

	_ = s.WriteMetric(ctx, "node1", "cpu", 42.5, ts1)
	_ = s.WriteMetric(ctx, "node1", "cpu", 50.0, ts2)
	_ = s.WriteMetric(ctx, "node1", "memory", 70.0, ts3)
	_ = s.Flush(ctx)

	result, err := s.QueryMetrics(ctx, []string{"cpu", "memory"},
		ts1.Add(-time.Hour), ts3.Add(time.Hour), "node1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(result["cpu"]) != 2 {
		t.Fatalf("expected 2 cpu points, got %d", len(result["cpu"]))
	}
	if result["cpu"][0].Value != 42.5 {
		t.Fatalf("expected 42.5, got %f", result["cpu"][0].Value)
	}
	if result["cpu"][1].Value != 50.0 {
		t.Fatalf("expected 50.0, got %f", result["cpu"][1].Value)
	}
	if len(result["memory"]) != 1 {
		t.Fatalf("expected 1 memory point, got %d", len(result["memory"]))
	}
}

func TestQueryMetrics_EmptyTimeRange(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()

	ts := time.Now().UTC()
	_ = s.WriteMetric(ctx, "node1", "cpu", 80.0, ts)
	_ = s.Flush(ctx)

	result, err := s.QueryMetrics(ctx, []string{"cpu"},
		ts.Add(time.Hour), ts.Add(2*time.Hour), "node1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result["cpu"]) != 0 {
		t.Fatalf("expected 0 points, got %d", len(result["cpu"]))
	}
}

func TestQueryMetrics_NoData(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	now := time.Now().UTC()

	result, err := s.QueryMetrics(ctx, []string{"cpu"},
		now.Add(-24*time.Hour), now, "nonexistent")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result["cpu"]) != 0 {
		t.Fatalf("expected 0 points, got %d", len(result["cpu"]))
	}
}

func TestQueryLatest(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()

	now := time.Now().UTC()
	_ = s.WriteMetric(ctx, "node2", "cpu", 33.3, now)
	_ = s.WriteMetric(ctx, "node2", "memory", 55.5, now)
	_ = s.WriteMetric(ctx, "node2", "disk", 20.0, now)
	_ = s.WriteMetric(ctx, "node2", "disk_used", 40.0, now)
	_ = s.WriteMetric(ctx, "node2", "disk_total", 100.0, now)
	_ = s.Flush(ctx)

	result, err := s.QueryLatest(ctx, "node2")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}

	if result["cpu"] != 33.3 {
		t.Fatalf("expected cpu 33.3, got %f", result["cpu"])
	}
	if result["memory"] != 55.5 {
		t.Fatalf("expected memory 55.5, got %f", result["memory"])
	}
	if result["disk"] != 20.0 {
		t.Fatalf("expected disk 20.0, got %f", result["disk"])
	}
	if result["disk_used"] != 40.0 || result["disk_total"] != 100.0 {
		t.Fatalf("disk capacity metrics = %#v", result)
	}
}

func TestQueryLatest_NoData(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()

	result, err := s.QueryLatest(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestMTSStore_Close(t *testing.T) {
	s := setupTestMTS(t)
	err := s.Close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = s.Close()
}

func TestWriteMultipleMetrics(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	now := time.Now().UTC()
	metrics := model.NodeMetrics{
		CPU: 99, CPUUsed: 99, CPUTotal: 99,
		Memory: 99, MemoryUsed: 99, MemoryTotal: 99,
		Disk: 99, DiskUsed: 99, DiskTotal: 99,
		DiskRead: 99, DiskWrite: 99,
		NetRecv: 99, NetSent: 99, NetRecvTotal: 99, NetSentTotal: 99,
		Swap: 99, SwapUsed: 99, SwapTotal: 99,
		Load1: 99, Load5: 99, Load15: 99,
		Uptime: 99, Processes: 99, TCPConnections: 99, UDPConnections: 99,
	}
	if err := s.WriteNodeMetrics(ctx, NodeMetricSample{
		NodeID: "node_multi", Metrics: metrics, Timestamp: now,
	}); err != nil {
		t.Fatalf("write node metrics: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flush node metrics: %v", err)
	}

	result, err := s.QueryLatest(ctx, "node_multi")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}

	for _, metric := range model.MetricNames() {
		v, ok := result[metric]
		if !ok {
			t.Fatalf("expected metric %s in result", metric)
		}
		if v != 99.0 {
			t.Fatalf("expected 99.0 for %s, got %f", metric, v)
		}
	}
}

func TestWriteMetric_AfterClose(t *testing.T) {
	s := setupTestMTS(t)
	_ = s.WriteMetric(context.Background(), "n", "cpu", 1.0, time.Now())
	_ = s.Close()
}

func TestQueryMetrics_AllKnownMetrics(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, metric := range knownMetrics {
		_ = s.WriteMetric(ctx, "node_full", metric, 123.0, now)
	}
	_ = s.Flush(ctx)

	result, err := s.QueryMetrics(ctx, knownMetrics,
		now.Add(-time.Hour), now.Add(time.Hour), "node_full")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	for _, metric := range knownMetrics {
		if len(result[metric]) != 1 {
			t.Fatalf("expected 1 point for %s, got %d", metric, len(result[metric]))
		}
	}
}
