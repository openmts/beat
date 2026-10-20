package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

type acceleratorMonitorFixture struct {
	err   error
	stats store.AcceleratorStats
}

func (f acceleratorMonitorFixture) Ready(context.Context) error { return f.err }

func (f acceleratorMonitorFixture) Stats(context.Context) (store.AcceleratorStats, error) {
	return f.stats, f.err
}

func TestAcceleratorReadinessAndMetricsAreBoundedAndTruthful(t *testing.T) {
	operations := &operability{}
	if check := operations.acceleratorCheck(t.Context()); check.Status != "ok" {
		t.Fatal(check)
	}
	var output strings.Builder
	operations.writeAcceleratorMetrics(&output, t.Context())
	if output.Len() != 0 {
		t.Fatal("unconfigured accelerator monitor emitted values")
	}
	operations.accelerators = acceleratorMonitorFixture{err: errors.New("private database path")}
	if check := operations.acceleratorCheck(t.Context()); check.Status != "error" || check.Detail != "unavailable" {
		t.Fatal(check)
	}
	operations.writeAcceleratorMetrics(&output, t.Context())
	if !strings.Contains(output.String(), "beat_accelerator_query_success 0") || strings.Contains(output.String(), "beat_accelerator_devices") || strings.Contains(output.String(), "private") {
		t.Fatal(output.String())
	}
	output.Reset()
	future := time.Now().Add(time.Hour)
	operations.accelerators = acceleratorMonitorFixture{stats: store.AcceleratorStats{ActiveDevices: 2, InactiveDevices: 1, Pending: 3, OldestPendingAt: &future}}
	if check := operations.acceleratorCheck(t.Context()); check.Status != "ok" {
		t.Fatal(check)
	}
	operations.writeAcceleratorMetrics(&output, t.Context())
	for _, expected := range []string{"beat_accelerator_query_success 1", "beat_accelerator_devices{state=\"active\"} 2", "beat_accelerator_devices{state=\"inactive\"} 1", "beat_accelerator_pending 3", "beat_accelerator_oldest_pending_seconds 0"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatal("missing bounded metric", expected)
		}
	}
}

func TestAcceleratorMonitorAndInitializationFailures(t *testing.T) {
	services := &serverServices{}
	if acceleratorMonitoring(services) != nil {
		t.Fatal("nil GPU service became a nonnil interface")
	}
	if err := services.initializeAccelerators(&serverStores{sqlite: &store.SQLiteStore{}}); err == nil {
		t.Fatal("missing MTS dependency accepted")
	}
	startAcceleratorWorker(backgroundConfig{services: services}, &backgroundTasks{})
}
