package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func renameTable(t *testing.T, db *sql.DB, from, to string) {
	t.Helper()
	if _, err := db.Exec("ALTER TABLE " + from + " RENAME TO " + to); err != nil {
		t.Fatalf("rename %s: %v", from, err)
	}
}

// TestNetworkTaskStoreFailsClosedOnUnavailableTables 覆盖网络任务在主子表不可用、
// 入参非法与行内类型被破坏时的失败关闭。
func TestNetworkTaskStoreFailsClosedOnUnavailableTables(t *testing.T) {
	instance := setupTestDB(t)
	ctx := context.Background()
	tasks := NewNetworkTaskStore(instance.DB)
	node, err := NewNodeStore(instance.DB).UpsertNode(ctx, "probe-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create probe node: %v", err)
	}
	task, err := tasks.CreateTask(ctx, validNetworkTask(), []string{node.ID})
	if err != nil {
		t.Fatalf("create network task: %v", err)
	}
	invalid := validNetworkTask()
	invalid.Name = ""
	if _, err := tasks.UpdateTask(ctx, task.ID, invalid, []string{node.ID}); err == nil {
		t.Fatal("invalid network task update was accepted")
	}

	renameTable(t, instance.DB, "network_tasks", "unavailable_network_tasks")
	if _, err := tasks.CreateTask(ctx, validNetworkTask(), []string{node.ID}); err == nil {
		t.Fatal("network task was created without its table")
	}
	if _, err := tasks.GetTask(ctx, task.ID); err == nil {
		t.Fatal("network task was read without its table")
	}
	if _, err := tasks.ListTasks(ctx, false); err == nil {
		t.Fatal("network tasks were listed without their table")
	}
	if _, err := tasks.DeleteTask(ctx, task.ID); err == nil {
		t.Fatal("network task was deleted without its table")
	}
	if err := tasks.UpdateSortOrder(ctx, []string{task.ID}); err == nil {
		t.Fatal("network tasks were sorted without their table")
	}
	renameTable(t, instance.DB, "unavailable_network_tasks", "network_tasks")

	renameTable(t, instance.DB, "nodes", "unavailable_nodes")
	if _, err := tasks.GetTask(ctx, task.ID); err == nil {
		t.Fatal("network task nodes were read without the node table")
	}
	if _, err := tasks.ListTasks(ctx, false); err == nil {
		t.Fatal("network tasks were listed without the node table")
	}
	if _, err := tasks.CreateTask(ctx, validNetworkTask(), []string{node.ID}); err == nil {
		t.Fatal("network task was created without the node table")
	}
	renameTable(t, instance.DB, "unavailable_nodes", "nodes")

	renameTable(t, instance.DB, "network_tasks", "backup_network_tasks")
	if _, err := instance.DB.Exec(
		"CREATE VIEW network_tasks AS SELECT id, name FROM backup_network_tasks"); err != nil {
		t.Fatalf("create narrow network task view: %v", err)
	}
	if _, err := tasks.ListTasks(ctx, false); err == nil {
		t.Fatal("network task row with missing columns was accepted")
	}
	if _, err := instance.DB.Exec("DROP VIEW network_tasks"); err != nil {
		t.Fatalf("drop narrow network task view: %v", err)
	}
	renameTable(t, instance.DB, "backup_network_tasks", "network_tasks")
}

// TestTrafficReportScheduleStoreFailsClosedOnUnavailableTables 覆盖定时报表调度
// 在调度表与目标关联表不可用时的失败关闭。
func TestTrafficReportScheduleStoreFailsClosedOnUnavailableTables(t *testing.T) {
	instance := setupTestDB(t)
	ctx := context.Background()
	schedules := NewTrafficReportScheduleStore(instance.DB)
	schedule := &model.TrafficReportSchedule{Name: "daily", Cadence: "daily", Timezone: "UTC",
		SendHour: 8, Weekday: 1, MonthDay: 1, AllNodes: true, AllChannels: true, Enabled: true,
		NextRunAt: time.Now().UTC().Add(time.Hour)}
	if _, err := schedules.Create(ctx, schedule); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if _, err := schedules.Update(ctx, schedule.ID, schedule); err != nil {
		t.Fatalf("update schedule: %v", err)
	}

	renameTable(t, instance.DB, "traffic_report_schedules", "unavailable_traffic_report_schedules")
	if _, err := schedules.Create(ctx, schedule); err == nil {
		t.Fatal("schedule was created without its table")
	}
	if _, err := schedules.Update(ctx, schedule.ID, schedule); err == nil {
		t.Fatal("schedule was updated without its table")
	}
	if _, err := schedules.Get(ctx, schedule.ID); err == nil {
		t.Fatal("schedule was read without its table")
	}
	if _, err := schedules.List(ctx); err == nil {
		t.Fatal("schedules were listed without their table")
	}
	if _, err := schedules.ListDue(ctx, time.Now().UTC()); err == nil {
		t.Fatal("due schedules were listed without their table")
	}
	if _, err := schedules.ClaimDue(ctx, schedule.ID, "period", time.Now().UTC(), time.Now().UTC()); err == nil {
		t.Fatal("schedule was claimed without its table")
	}
	if err := schedules.CompleteRun(ctx, schedule.ID, time.Now().UTC(),
		model.TrafficReportDeliveryStatus{State: "delivered"}); err == nil {
		t.Fatal("run completed without the schedule table")
	}
	if err := schedules.Delete(ctx, schedule.ID); err == nil {
		t.Fatal("schedule was deleted without its table")
	}
	renameTable(t, instance.DB, "unavailable_traffic_report_schedules", "traffic_report_schedules")

	renameTable(t, instance.DB, "traffic_report_schedule_nodes", "unavailable_traffic_report_nodes")
	if _, err := schedules.Create(ctx, schedule); err == nil {
		t.Fatal("schedule was created without its target table")
	}
	if err := schedules.Delete(ctx, schedule.ID); err == nil {
		t.Fatal("schedule was deleted without its target table")
	}
	renameTable(t, instance.DB, "unavailable_traffic_report_nodes", "traffic_report_schedule_nodes")

	renameTable(t, instance.DB, "traffic_report_schedules", "backup_traffic_report_schedules")
	if _, err := instance.DB.Exec(
		"CREATE VIEW traffic_report_schedules AS SELECT id, name FROM backup_traffic_report_schedules"); err != nil {
		t.Fatalf("create narrow schedule view: %v", err)
	}
	if _, err := schedules.List(ctx); err == nil {
		t.Fatal("schedule row with missing columns was accepted")
	}
	if _, err := instance.DB.Exec("DROP VIEW traffic_report_schedules"); err != nil {
		t.Fatalf("drop narrow schedule view: %v", err)
	}
	renameTable(t, instance.DB, "backup_traffic_report_schedules", "traffic_report_schedules")
}

// TestGroupStoreFailsClosedOnUnavailableTables 覆盖分组删除、默认分组与排序
// 在分组表或节点表不可用时的失败关闭。
func TestGroupStoreFailsClosedOnUnavailableTables(t *testing.T) {
	instance := setupTestDB(t)
	ctx := context.Background()
	groups := NewGroupStore(instance.DB)
	group, err := groups.CreateGroup(ctx, "Fixture group")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	renameTable(t, instance.DB, "groups", "unavailable_groups")
	if err := groups.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("group was deleted without its table")
	}
	if err := groups.SetDefaultGroup(ctx, group.ID); err == nil {
		t.Fatal("default group was set without the group table")
	}
	if err := groups.UpdateSortOrder(ctx, []string{group.ID}); err == nil {
		t.Fatal("groups were sorted without their table")
	}
	renameTable(t, instance.DB, "unavailable_groups", "groups")

	renameTable(t, instance.DB, "nodes", "unavailable_nodes")
	if err := groups.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("group was deleted without the node table")
	}
	renameTable(t, instance.DB, "unavailable_nodes", "nodes")

	if err := groups.SetDefaultGroup(ctx, "missing-group"); err == nil {
		t.Fatal("missing group became the default")
	}
	if err := groups.DeleteGroup(ctx, "missing-group"); err == nil {
		t.Fatal("missing group was deleted")
	}
}
