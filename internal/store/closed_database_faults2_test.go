package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEnrollmentStoresFailClosedWithoutDatabase 覆盖节点入网在数据库不可用时的失败关闭。
func TestEnrollmentStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	enrollment := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
	if _, err := enrollment.ListGrants(ctx); err == nil {
		t.Fatal("closed database listed enrollment grants")
	}
	if _, err := enrollment.Grant(ctx, "grant"); err == nil {
		t.Fatal("closed database returned an enrollment grant")
	}
	if _, err := enrollment.EnrollmentRequest(ctx, "request"); err == nil {
		t.Fatal("closed database returned an enrollment request")
	}
	if _, err := enrollment.ListEnrollmentRequests(ctx, EnrollmentRequestFilter{}); err == nil {
		t.Fatal("closed database listed enrollment requests")
	}
	if _, err := enrollment.EnrollmentStatistics(ctx, time.Now().UTC()); err == nil {
		t.Fatal("closed database summarised enrollment")
	}
	if _, err := enrollment.CleanupEnrollmentState(ctx, time.Now().UTC()); err == nil {
		t.Fatal("closed database cleaned enrollment state")
	}
}

// TestAlertEvaluationStoresFailClosedWithoutDatabase 覆盖告警评估读取在数据库不可用时的失败关闭。
func TestAlertEvaluationStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	database := closedSQLiteStore(t)
	rules := NewAlertEvaluationRuleStore(database.DB)
	evaluations := NewAlertEvaluationStore(database.DB)
	events := NewAlertEventStore(database.DB)
	if _, err := rules.List(ctx); err == nil {
		t.Fatal("closed database listed evaluation rules")
	}
	if _, err := evaluations.List(ctx, AlertEvaluationQuery{}); err == nil {
		t.Fatal("closed database listed evaluations")
	}
	if _, err := events.ListAlertEventsPage(ctx, AlertEventQuery{}); err == nil {
		t.Fatal("closed database listed alert events")
	}
}

// TestBackupUploadStoresFailClosedWithoutDatabase 覆盖可续传备份上传在数据库不可用时的失败关闭。
func TestBackupUploadStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	uploads := NewBackupUploadStore(closedSQLiteStore(t).DB)
	if _, err := uploads.Get(ctx, "session"); err == nil {
		t.Fatal("closed database returned an upload session")
	}
	if _, err := uploads.ListByOwner(ctx, "owner", 10); err == nil {
		t.Fatal("closed database listed owner sessions")
	}
	if _, err := uploads.ListQuarantined(ctx, 10); err == nil {
		t.Fatal("closed database listed quarantined sessions")
	}
	if _, err := uploads.ListChunks(ctx, "session"); err == nil {
		t.Fatal("closed database listed upload chunks")
	}
	if _, err := uploads.Chunk(ctx, "session", 0); err == nil {
		t.Fatal("closed database returned an upload chunk")
	}
	if _, err := uploads.PendingStagingCleanup(ctx, 10); err == nil {
		t.Fatal("closed database listed staging cleanup")
	}
	if _, err := uploads.Expire(ctx, time.Now().UTC()); err == nil {
		t.Fatal("closed database expired upload sessions")
	}
	if _, err := uploads.ListQuarantined(ctx, 0); err == nil {
		t.Fatal("out-of-range page limit must be rejected")
	}
}

// TestMetricErasureStoresFailClosedWithoutDatabase 覆盖指标擦除在数据库不可用时的失败关闭。
func TestMetricErasureStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	erasures := NewMetricErasureStore(closedSQLiteStore(t).DB)
	now, jobID := time.Now().UTC(), uuid.NewString()
	if _, err := erasures.Get(ctx, jobID); err == nil {
		t.Fatal("closed database returned an erasure job")
	}
	if _, err := erasures.List(ctx, 10); err == nil {
		t.Fatal("closed database listed erasure jobs")
	}
	if _, err := erasures.Status(ctx); err == nil {
		t.Fatal("closed database summarised erasure jobs")
	}
	if err := erasures.Cancel(ctx, jobID, now); err == nil {
		t.Fatal("closed database cancelled an erasure job")
	}
	if err := erasures.Retry(ctx, jobID, now); err == nil {
		t.Fatal("closed database retried an erasure job")
	}
}

// TestTrafficCalibrationStoresFailClosedWithoutDatabase 覆盖流量校准读取在数据库不可用时的失败关闭。
func TestTrafficCalibrationStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	ledger := NewTrafficCalibrationStore(closedSQLiteStore(t).DB)
	start := time.Now().UTC()
	window := TrafficCalibrationWindow{NodeID: uuid.NewString(), Start: start, End: start.Add(time.Hour)}
	if _, err := ledger.LatestState(ctx, window); err == nil {
		t.Fatal("closed database returned calibration state")
	}
	if _, err := ledger.Event(ctx, uuid.NewString(), uuid.NewString()); err == nil {
		t.Fatal("closed database returned a calibration event")
	}
	if _, err := ledger.History(ctx, TrafficCalibrationHistoryRequest{NodeID: uuid.NewString(), Limit: 10}); err == nil {
		t.Fatal("closed database listed calibration history")
	}
}
