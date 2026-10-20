package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/beat/backend/internal/notificationqueue"
)

func (operations *operability) writeNotificationQueueMetrics(output *strings.Builder, ctx context.Context) {
	queue, ok := operations.notifications.(interface {
		Snapshot(context.Context) (notificationqueue.QueueSnapshot, error)
	})
	if !ok {
		return
	}
	snapshot, err := queue.Snapshot(ctx)
	if err != nil {
		return
	}
	_, _ = output.WriteString("# TYPE beat_notification_queue_depth gauge\n")
	for _, entry := range []struct {
		state string
		count int
	}{{"pending", snapshot.Counts.Pending}, {"leased", snapshot.Counts.Leased}, {"succeeded", snapshot.Counts.Succeeded}, {"dead", snapshot.Counts.Dead}, {"cancelled", snapshot.Counts.Cancelled}} {
		_, _ = fmt.Fprintf(output, "beat_notification_queue_depth{state=%q} %d\n", entry.state, entry.count)
	}
	_, _ = fmt.Fprintf(output, "# TYPE beat_notification_oldest_pending_seconds gauge\nbeat_notification_oldest_pending_seconds %g\n", snapshot.OldestPendingAgeSeconds)
	_, _ = fmt.Fprintf(output, "# TYPE beat_notification_open_circuits gauge\nbeat_notification_open_circuits %d\n", snapshot.OpenCircuits)
	_, _ = fmt.Fprintf(output, "# TYPE beat_notification_sqlite_bytes gauge\nbeat_notification_sqlite_bytes %d\n", snapshot.SQLiteBytes)
	_, _ = fmt.Fprintf(output, "# TYPE beat_notification_sqlite_reusable_bytes gauge\nbeat_notification_sqlite_reusable_bytes %d\n", snapshot.SQLiteReusableBytes)
	if snapshot.FilesystemFreeBytes != nil {
		_, _ = fmt.Fprintf(output, "# TYPE beat_notification_storage_free_bytes gauge\nbeat_notification_storage_free_bytes %d\n", *snapshot.FilesystemFreeBytes)
	}
	operations.writeNotificationDispatchMetrics(output)
}

func (operations *operability) writeNotificationDispatchMetrics(output *strings.Builder) {
	queue, ok := operations.notifications.(interface {
		DispatchMetrics() []notificationqueue.ProviderDispatchMetrics
	})
	if !ok {
		return
	}
	_, _ = output.WriteString("# TYPE beat_notification_attempts_total counter\n# TYPE beat_notification_retries_total counter\n# TYPE beat_notification_dead_total counter\n# TYPE beat_notification_dispatch_seconds histogram\n")
	for _, metrics := range queue.DispatchMetrics() {
		writeProviderDispatchMetrics(output, metrics)
	}
}

func writeProviderDispatchMetrics(output *strings.Builder, metrics notificationqueue.ProviderDispatchMetrics) {
	for _, value := range []struct {
		outcome string
		count   uint64
	}{{"succeeded", metrics.Succeeded}, {"retryable", metrics.Retryable}, {"permanent", metrics.Permanent}} {
		_, _ = fmt.Fprintf(output, "beat_notification_attempts_total{provider=%q,outcome=%q} %d\n", metrics.Provider, value.outcome, value.count)
	}
	_, _ = fmt.Fprintf(output, "beat_notification_retries_total{provider=%q} %d\n", metrics.Provider, metrics.Retried)
	_, _ = fmt.Fprintf(output, "beat_notification_dead_total{provider=%q} %d\n", metrics.Provider, metrics.Dead)
	for index, bound := range notificationqueue.DispatchDurationBounds() {
		_, _ = fmt.Fprintf(output, "beat_notification_dispatch_seconds_bucket{provider=%q,le=%q} %d\n", metrics.Provider, fmt.Sprint(bound.Seconds()), metrics.DurationBucketCount[index])
	}
	_, _ = fmt.Fprintf(output, "beat_notification_dispatch_seconds_bucket{provider=%q,le=\"+Inf\"} %d\n", metrics.Provider, metrics.DurationCount)
	_, _ = fmt.Fprintf(output, "beat_notification_dispatch_seconds_count{provider=%q} %d\n", metrics.Provider, metrics.DurationCount)
	_, _ = fmt.Fprintf(output, "beat_notification_dispatch_seconds_sum{provider=%q} %g\n", metrics.Provider, metrics.DurationSeconds)
}
