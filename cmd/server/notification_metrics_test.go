package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/notificationqueue"
)

type queueMetricsFixture struct {
	failed bool
}

func (queueMetricsFixture) Stats() notification.DeliveryStats { return notification.DeliveryStats{} }

func (fixture queueMetricsFixture) Snapshot(context.Context) (notificationqueue.QueueSnapshot, error) {
	if fixture.failed {
		return notificationqueue.QueueSnapshot{}, errors.New("unavailable")
	}
	free := uint64(1024)
	return notificationqueue.QueueSnapshot{SQLiteBytes: 512, FilesystemFreeBytes: &free}, nil
}

func (queueMetricsFixture) DispatchMetrics() []notificationqueue.ProviderDispatchMetrics {
	return []notificationqueue.ProviderDispatchMetrics{{Provider: "webhook", Succeeded: 1, Retryable: 2, Retried: 1, Dead: 1, DurationCount: 3}}
}

func TestNotificationMetricsIncludeBoundedProviderAndQueueSeries(t *testing.T) {
	operations := &operability{notifications: queueMetricsFixture{}}
	var output strings.Builder
	operations.writeNotificationQueueMetrics(&output, t.Context())
	for _, expected := range []string{
		`beat_notification_attempts_total{provider="webhook",outcome="succeeded"} 1`,
		`beat_notification_retries_total{provider="webhook"} 1`,
		`beat_notification_dead_total{provider="webhook"} 1`,
		`beat_notification_dispatch_seconds_bucket{provider="webhook",le="+Inf"} 3`,
		`beat_notification_storage_free_bytes 1024`,
		`beat_notification_sqlite_bytes 512`,
		`beat_notification_queue_depth{state="pending"} 0`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("metric missing: %s", expected)
		}
	}
	operations.notifications = queueMetricsFixture{failed: true}
	output.Reset()
	operations.writeNotificationQueueMetrics(&output, t.Context())
	if output.Len() != 0 {
		t.Fatal("failed queue snapshot returned misleading metrics")
	}
	operations.notifications = fakeNotificationMonitor{}
	operations.writeNotificationDispatchMetrics(&output)
	operations.writeNotificationQueueMetrics(&output, t.Context())
}
