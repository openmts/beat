package store

import (
	"context"
	"strings"
	"testing"

	"github.com/openmts/mts"
)

func gpuMemoryPoint(nodeID, device string, at int64, value float64) mts.Point {
	return mts.Point{Measurement: "gpu_memory_used_bytes", Timestamp: at,
		Tags:   map[string]string{"node": nodeID, "device": device, "vendor": "nvidia"},
		Fields: map[string]mts.FieldValue{"value": mts.Float64Value(value)}}
}

func gpuCapacityPoint(nodeID, device string, at int64, value float64) mts.Point {
	point := gpuMemoryPoint(nodeID, device, at, value)
	point.Measurement = "gpu_memory_total_bytes"
	return point
}

func TestAcceleratorArchiveMemoryValidation(t *testing.T) {
	f := newAcceleratorFixture(t)
	if err := f.metrics.ValidateAcceleratorArchiveMemory(t.Context()); err != nil {
		t.Fatalf("empty archive reported %v", err)
	}
	node, device := f.node.ID, "device-1"
	if err := f.metrics.ImportPoints(t.Context(), []mts.Point{
		gpuMemoryPoint(node, device, 1_000, 5),
		gpuCapacityPoint(node, device, 1_000, 10),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.metrics.ValidateAcceleratorArchiveMemory(t.Context()); err != nil {
		t.Fatalf("usage below capacity reported %v", err)
	}
	if err := f.metrics.ImportPoints(t.Context(), []mts.Point{
		gpuMemoryPoint(node, device, 2_000, 20),
		gpuCapacityPoint(node, device, 2_000, 10),
	}); err != nil {
		t.Fatal(err)
	}
	err := f.metrics.ValidateAcceleratorArchiveMemory(t.Context())
	if err == nil || !strings.Contains(err.Error(), "exceeds capacity") {
		t.Fatalf("usage above capacity = %v", err)
	}
}

func TestAcceleratorArchiveMemoryHandlesMissingCapacityAndCancellation(t *testing.T) {
	f := newAcceleratorFixture(t)
	node := f.node.ID
	if err := f.metrics.ImportPoints(t.Context(), []mts.Point{
		gpuMemoryPoint(node, "device-1", 1_000, 9),
		gpuCapacityPoint(node, "device-1", 1_000, 10),
		gpuMemoryPoint(node, "device-2", 1_000, 7),
		gpuCapacityPoint(node, "device-2", 1_000, 10),
		gpuMemoryPoint(node, "device-3", 1_000, 4),
	}); err != nil {
		t.Fatal(err)
	}
	// 独立保留策略可能已经删掉了配对的 total 行：缺失项不能被当作 0，也不能报错。
	if err := f.metrics.ValidateAcceleratorArchiveMemory(t.Context()); err != nil {
		t.Fatalf("missing capacity row reported %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.metrics.ValidateAcceleratorArchiveMemory(cancelled); err == nil {
		t.Fatal("cancelled archive validation succeeded")
	}
}

func TestAcceleratorArchiveMemoryRejectsSecondDeviceViolation(t *testing.T) {
	f := newAcceleratorFixture(t)
	node := f.node.ID
	if err := f.metrics.ImportPoints(t.Context(), []mts.Point{
		gpuMemoryPoint(node, "device-1", 5_000, 1),
		gpuCapacityPoint(node, "device-1", 5_000, 10),
		gpuMemoryPoint(node, "device-2", 5_000, 11),
		gpuCapacityPoint(node, "device-2", 5_000, 10),
	}); err != nil {
		t.Fatal(err)
	}
	err := f.metrics.ValidateAcceleratorArchiveMemory(t.Context())
	if err == nil || !strings.Contains(err.Error(), "exceeds capacity") {
		t.Fatalf("second device violation = %v", err)
	}
}
