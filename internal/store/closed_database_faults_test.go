package store

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// TestDashboardCollaborationStoresFailClosedWithoutDatabase 覆盖看板协作
// （快照、面板库、版本、注释、面板告警）在数据库不可用时的失败关闭契约。
func TestDashboardCollaborationStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	database := closedSQLiteStore(t)
	snapshots := NewDashboardSnapshotStore(database.DB)
	library := NewDashboardPanelLibraryStore(database.DB)
	dashboards := NewDashboardStore(database.DB)
	annotations := NewDashboardAnnotationStore(database.DB)
	panelAlerts := NewDashboardPanelAlertStore(database.DB)

	if _, err := snapshots.List(ctx, "user"); err == nil {
		t.Fatal("closed database listed snapshots")
	}
	if _, err := snapshots.GetByToken(ctx, "token"); err == nil {
		t.Fatal("closed database resolved a snapshot token")
	}
	if _, err := snapshots.Get(ctx, "id"); err == nil {
		t.Fatal("closed database returned a snapshot")
	}
	if err := snapshots.Revoke(ctx, "id", DashboardActor{UserID: "user"}); err == nil {
		t.Fatal("closed database revoked a snapshot")
	}
	if _, err := library.List(ctx, "user"); err == nil {
		t.Fatal("closed database listed library panels")
	}
	if _, err := library.Get(ctx, "id"); err == nil {
		t.Fatal("closed database returned a library panel")
	}
	if _, err := dashboards.ListVersions(ctx, "dashboard"); err == nil {
		t.Fatal("closed database listed dashboard versions")
	}
	if _, err := dashboards.VersionDefinition(ctx, "dashboard", 1); err == nil {
		t.Fatal("closed database returned a dashboard version")
	}
	if _, err := annotations.List(ctx, AnnotationFilter{}); err == nil {
		t.Fatal("closed database listed annotations")
	}
	if _, err := panelAlerts.List(ctx, "dashboard"); err == nil {
		t.Fatal("closed database listed panel alert bindings")
	}
}

// TestTrafficReportScheduleStoreFailsClosedWithoutDatabase 覆盖定时报表调度在
// 数据库不可用时的读写失败关闭。
func TestTrafficReportScheduleStoreFailsClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	database := closedSQLiteStore(t)
	schedules := NewTrafficReportScheduleStore(database.DB)
	now := time.Now().UTC()
	if _, err := schedules.List(ctx); err == nil {
		t.Fatal("closed database listed schedules")
	}
	if _, err := schedules.ListDue(ctx, now); err == nil {
		t.Fatal("closed database listed due schedules")
	}
	if _, err := schedules.Get(ctx, "id"); err == nil {
		t.Fatal("closed database returned a schedule")
	}
	if err := schedules.Delete(ctx, "id"); err == nil {
		t.Fatal("closed database deleted a schedule")
	}
	if _, err := schedules.ClaimDue(ctx, "id", "period", now, now); err == nil {
		t.Fatal("closed database claimed a schedule")
	}
	if err := schedules.CompleteRun(ctx, "id", now, model.TrafficReportDeliveryStatus{State: "delivered"}); err == nil {
		t.Fatal("closed database completed a run")
	}
}

// TestCustomMetricAndAcceleratorStoresFailClosedWithoutDatabase 覆盖自定义指标与
// 加速卡库在数据库不可用时的读取失败关闭。
func TestCustomMetricAndAcceleratorStoresFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	database := closedSQLiteStore(t)
	metrics := NewCustomMetricStore(database.DB)
	accelerators := NewAcceleratorStore(database.DB)
	if _, err := metrics.List(ctx); err == nil {
		t.Fatal("closed database listed custom metrics")
	}
	if _, err := metrics.Get(ctx, "id"); err == nil {
		t.Fatal("closed database returned a custom metric")
	}
	if _, err := metrics.ListEnabledForNode(ctx, "node"); err == nil {
		t.Fatal("closed database listed node metrics")
	}
	if _, err := metrics.NodeMetricIDs(ctx, "node"); err == nil {
		t.Fatal("closed database listed node metric ids")
	}
	if _, err := metrics.NodeIDsForMetric(ctx, "metric"); err == nil {
		t.Fatal("closed database listed metric nodes")
	}
	if err := metrics.Delete(ctx, "id"); err == nil {
		t.Fatal("closed database deleted a custom metric")
	}
	if err := metrics.SetNodeBindings(ctx, "node", []string{"metric"}); err == nil {
		t.Fatal("closed database bound node metrics")
	}
	if err := metrics.SetMetricNodes(ctx, "metric", []string{"node"}); err == nil {
		t.Fatal("closed database bound metric nodes")
	}
	if _, err := accelerators.Pending(ctx, "node"); err == nil {
		t.Fatal("closed database returned a pending accelerator report")
	}
	if _, err := accelerators.PendingNodes(ctx); err == nil {
		t.Fatal("closed database listed pending accelerator nodes")
	}
}

// TestNetworkTaskStoreFailsClosedWithoutDatabase 覆盖探测任务读取与排序在
// 数据库不可用时的失败关闭。
func TestNetworkTaskStoreFailsClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	database := closedSQLiteStore(t)
	tasks := NewNetworkTaskStore(database.DB)
	if _, err := tasks.GetTask(ctx, "id"); err == nil {
		t.Fatal("closed database returned a network task")
	}
	if _, err := tasks.ListTasks(ctx, false); err == nil {
		t.Fatal("closed database listed network tasks")
	}
	if _, err := tasks.DeleteTask(ctx, "id"); err == nil {
		t.Fatal("closed database deleted a network task")
	}
	if err := tasks.UpdateSortOrder(ctx, []string{"id"}); err == nil {
		t.Fatal("closed database sorted network tasks")
	}
	if _, err := tasks.ListAssignments(ctx, "node"); err == nil {
		t.Fatal("closed database listed task assignments")
	}
	if _, err := tasks.IsTaskAssignedToNode(ctx, "task", "node"); err == nil {
		t.Fatal("closed database answered an assignment query")
	}
}
