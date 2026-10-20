package notificationqueue

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAttemptMetricsUseFixedLabelsAndCumulativeDurationBuckets(t *testing.T) {
	s, _ := queueFixture(t)
	for _, input := range []attemptObservation{
		{lease: deliveryLease{ChannelType: "webhook", Attempt: 1}, result: model.NotificationSendResult{Success: true}, duration: time.Millisecond},
		{lease: deliveryLease{ChannelType: "webhook", Attempt: 2}, result: model.NotificationSendResult{Retryable: true}, duration: time.Second},
		{lease: deliveryLease{ChannelType: "webhook", Attempt: 6}, result: model.NotificationSendResult{Retryable: true}, duration: 31 * time.Second},
		{lease: deliveryLease{ChannelType: "PRIVATE_PROVIDER"}, result: model.NotificationSendResult{ErrorCode: "PRIVATE_ERROR"}},
	} {
		s.recordAttempt(input)
	}
	metrics := s.DispatchMetrics()
	webhook := metrics[0]
	if webhook.Provider != "webhook" || webhook.Succeeded != 1 || webhook.Retryable != 2 || webhook.Retried != 1 || webhook.Dead != 1 {
		t.Fatal("outcome counters did not distinguish retryable attempts from scheduled retries")
	}
	if webhook.DurationCount != 3 || webhook.DurationBucketCount[0] != 1 || webhook.DurationBucketCount[2] != 2 || webhook.DurationSeconds < 32 {
		t.Fatal("duration histogram did not preserve cumulative bucket semantics")
	}
	unknown := metrics[len(metrics)-1]
	if unknown.Provider != "unknown" || unknown.Permanent != 1 || unknown.Dead != 1 {
		t.Fatal("unknown provider created an unbounded metric label")
	}
	metrics[0].Provider = "changed"
	bounds := DispatchDurationBounds()
	bounds[0] = time.Hour
	if s.DispatchMetrics()[0].Provider != "webhook" || DispatchDurationBounds()[0] == time.Hour {
		t.Fatal("metric snapshot exposed mutable service state")
	}
}

func TestQueueSnapshotIncludesSQLiteAndStoragePressure(t *testing.T) {
	s, _ := queueFixture(t)
	snapshot, err := s.Snapshot(t.Context())
	if err != nil || snapshot.SQLiteBytes <= 0 || snapshot.SQLiteReusableBytes < 0 || snapshot.FilesystemFreeBytes == nil {
		t.Fatal("queue storage pressure is not observable")
	}
}
