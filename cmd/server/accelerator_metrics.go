package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/store"
)

type acceleratorMonitor interface {
	Ready(context.Context) error
	Stats(context.Context) (store.AcceleratorStats, error)
}

func (operations *operability) acceleratorCheck(ctx context.Context) readinessCheck {
	if operations.accelerators != nil {
		if err := operations.accelerators.Ready(ctx); err != nil {
			return readinessCheck{Status: "error", Detail: "unavailable"}
		}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) writeAcceleratorMetrics(output *strings.Builder, ctx context.Context) {
	if operations.accelerators == nil {
		return
	}
	stats, err := operations.accelerators.Stats(ctx)
	_, _ = fmt.Fprintf(output, "# TYPE beat_accelerator_query_success gauge\nbeat_accelerator_query_success %d\n", boolMetric(err == nil))
	if err != nil {
		return
	}
	_, _ = output.WriteString("# TYPE beat_accelerator_devices gauge\n")
	_, _ = fmt.Fprintf(output, "beat_accelerator_devices{state=\"active\"} %d\nbeat_accelerator_devices{state=\"inactive\"} %d\n", stats.ActiveDevices, stats.InactiveDevices)
	_, _ = fmt.Fprintf(output, "# TYPE beat_accelerator_pending gauge\nbeat_accelerator_pending %d\n", stats.Pending)
	age := 0.0
	if stats.OldestPendingAt != nil {
		age = max(0, time.Since(*stats.OldestPendingAt).Seconds())
	}
	_, _ = fmt.Fprintf(output, "# TYPE beat_accelerator_oldest_pending_seconds gauge\nbeat_accelerator_oldest_pending_seconds %g\n", age)
}
