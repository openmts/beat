package tsdb

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"beat/internal/domain"

	mts "github.com/openmts/mts"
)

func TestMTSRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := OpenMTS(ctx, t.TempDir(), 24*time.Hour, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("OpenMTS() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(ctx) })

	now := time.Now().UTC().Truncate(time.Second)
	want := domain.Metrics{
		Timestamp: now, CPUUsage: 42.5, CPUUser: 30, CPUSystem: 12.5,
		MemoryUsedBytes: 8 << 30, MemoryTotalBytes: 16 << 30, MemoryUsage: 50,
		DiskUsedBytes: 100 << 30, DiskTotalBytes: 200 << 30, DiskUsage: 50,
		NetworkRxBps: 1024, NetworkTxBps: 2048, Load1: 1.5, Load5: 1.2,
		Load15: 0.9, Processes: 120, TCPConnections: 42, UptimeSeconds: 3600,
	}
	if err := store.Append(ctx, "node-1", want); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	got, err := store.Query(ctx, "node-1", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query() returned %d points, want 1", len(got))
	}
	if got[0].CPUUsage != want.CPUUsage || got[0].MemoryUsage != want.MemoryUsage || got[0].DiskUsage != want.DiskUsage {
		t.Fatalf("round trip metrics = %+v, want %+v", got[0], want)
	}
	if err := store.DeleteNode(ctx, "node-1"); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}
	got, err = store.Query(ctx, "node-1", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query() after DeleteNode error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Query() after DeleteNode returned %d points, want 0", len(got))
	}
}

func TestMTSStatsIncludesDataDirectorySize(t *testing.T) {
	ctx := context.Background()
	store, err := OpenMTS(ctx, t.TempDir(), 24*time.Hour, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("OpenMTS() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(ctx) })

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.Append(ctx, "node-1", domain.Metrics{Timestamp: now, CPUUsage: 12.5}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := store.engine.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.SizeBytes <= 0 {
		t.Fatalf("Stats().SizeBytes = %d, want > 0", stats.SizeBytes)
	}
}

func TestMTSNetworkProbeRoundTripAndDelete(t *testing.T) {
	ctx := context.Background()
	store, err := OpenMTS(ctx, t.TempDir(), 24*time.Hour, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("OpenMTS() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(ctx) })

	now := time.Now().UTC().Truncate(time.Second)
	want := domain.NetworkProbePoint{
		Timestamp: now, TaskID: "task-1", TaskName: "三网质量巡检", Carrier: "telecom", Label: "电信目标",
		LossAvailable: true, LossPercent: 0, Sent: 3, Received: 3,
		LatencyAvailable: true, LatencyMinMs: 1.2, LatencyAvgMs: 1.8, LatencyMaxMs: 2.4,
		LatencyP50Ms: 1.9, LatencyP95Ms: 2.3, LatencyJitterMs: 0.4,
	}
	if err := store.AppendNetworkProbes(ctx, "node-1", []domain.NetworkProbePoint{want}); err != nil {
		t.Fatalf("AppendNetworkProbes() error = %v", err)
	}
	got, err := store.QueryNetworkProbes(ctx, "node-1", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("QueryNetworkProbes() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("QueryNetworkProbes() returned %d points, want 1", len(got))
	}
	point := got[0]
	if point.TaskID != want.TaskID || point.Carrier != want.Carrier || point.Label != want.Label || !point.LossAvailable || !point.LatencyAvailable || point.LatencyAvgMs != want.LatencyAvgMs {
		t.Fatalf("network probe round trip = %+v, want %+v", point, want)
	}
	if err := store.DeleteNode(ctx, "node-1"); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}
	got, err = store.QueryNetworkProbes(ctx, "node-1", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("QueryNetworkProbes() after DeleteNode error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("QueryNetworkProbes() after DeleteNode returned %d points, want 0", len(got))
	}
}

func TestMTSAppendTaskMetricsUsesConfiguredMeasurement(t *testing.T) {
	ctx := context.Background()
	store, err := OpenMTS(ctx, t.TempDir(), 24*time.Hour, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("OpenMTS() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(ctx) })

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.AppendTaskMetrics(ctx, "node-1", "app_custom_metrics", "task-1", "应用指标", now, map[string]float64{"qps": 118.7, "connections": 42}); err != nil {
		t.Fatalf("AppendTaskMetrics() error = %v", err)
	}
	query, err := mts.NewQuery().
		From("", "", "app_custom_metrics").
		Where(mts.TagEq("node_id", "node-1")).
		TimeRangeTime(now.Add(-time.Minute), now.Add(time.Minute)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.engine.QueryRows(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("custom measurement rows = %d, want 1", len(rows))
	}
	if rows[0].Tags["task_id"] != "task-1" || floatField(rows[0].Fields, "qps") != 118.7 || floatField(rows[0].Fields, "connections") != 42 {
		t.Fatalf("unexpected custom metric row: %+v", rows[0])
	}
}
