package service

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestRegisterNodeWritesAllMetricsToMTS(t *testing.T) {
	svc := setupTestNodeService(t)
	ctx := t.Context()
	metrics := &model.NodeMetrics{
		CPU: 50, CPUUsed: 4, CPUTotal: 8,
		Memory: 60, MemoryUsed: 6, MemoryTotal: 10,
		Disk: 40, DiskUsed: 40, DiskTotal: 100,
		DiskRead: 10, DiskWrite: 20,
		NetRecv: 100, NetSent: 200, NetRecvTotal: 1_000, NetSentTotal: 2_000,
		Swap: 25, SwapUsed: 1, SwapTotal: 4,
		Load1: 1, Load5: 2, Load15: 3,
		Uptime: 3_600, Processes: 42, TCPConnections: 7, UDPConnections: 3,
	}

	node, err := svc.RegisterNode(ctx, "test-host-metrics", 22, metrics)
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	if err := svc.mtsStore.Flush(ctx); err != nil {
		t.Fatalf("flush metrics: %v", err)
	}

	now := time.Now().UTC()
	got, err := svc.mtsStore.QueryMetrics(
		ctx,
		model.MetricNames(),
		now.Add(-time.Hour),
		now.Add(time.Hour),
		node.ID,
	)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}
	for _, metric := range metrics.TimeSeries() {
		points := got[metric.Name]
		if len(points) != 1 || points[0].Value != metric.Value {
			t.Fatalf("metric %s = %#v, want one point with value %v", metric.Name, points, metric.Value)
		}
	}
}
