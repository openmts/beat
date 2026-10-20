package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

// TestTrafficReportTestRunIsAuditedInsideTheCallerTransaction 覆盖手动测试发送的
// 事务内路径：成功必须留下审计行，非法输入与未知调度必须在调用方事务里失败关闭。
func TestTrafficReportTestRunIsAuditedInsideTheCallerTransaction(t *testing.T) {
	database := setupTestDB(t)
	schedules := NewTrafficReportScheduleStore(database.DB)
	now := model.NowUTC()
	schedule, err := schedules.Create(t.Context(), &model.TrafficReportSchedule{
		Name: "Coverage", Cadence: model.TrafficReportDaily, Timezone: "UTC", SendHour: 8,
		Weekday: 1, MonthDay: 1, AllNodes: true, AllChannels: true, Enabled: true, NextRunAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := catalogPolicyWrite(t, NewMetricCatalogStore(database.DB), model.MetricPolicyUpdate{})
	actor := EventMutationActor{UserID: owner.Audit.ActorID, SessionID: owner.SessionID}
	transaction := alertTestTx(t, database.DB)
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{ScheduleID: schedule.ID, Actor: actor, At: now}); err != nil {
		t.Fatalf("record traffic report test run: %v", err)
	}
	if err := schedules.RecordTestRunTx(t.Context(), nil, TrafficReportTest{ScheduleID: schedule.ID, Actor: actor, At: now}); err == nil {
		t.Fatal("nil transaction must be rejected")
	}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{Actor: actor, At: now}); err == nil {
		t.Fatal("missing schedule id must be rejected")
	}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{ScheduleID: schedule.ID, Actor: actor}); err == nil {
		t.Fatal("missing timestamp must be rejected")
	}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{ScheduleID: "missing", Actor: actor, At: now}); err == nil {
		t.Fatal("unknown schedule must be rejected")
	}
	rejected := EventMutationActor{UserID: "ghost", SessionID: "ghost"}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{ScheduleID: schedule.ID, Actor: rejected, At: now}); err == nil {
		t.Fatal("actor without a live session must be rejected")
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var action, outcome, resourceID string
	if err := database.DB.QueryRow(`SELECT action, outcome, resource_id FROM admin_audit_events
		WHERE action='traffic_report.test_queued'`).Scan(&action, &outcome, &resourceID); err != nil {
		t.Fatalf("test run audit: %v", err)
	}
	if action != "traffic_report.test_queued" || outcome != model.AuditOutcomeSuccess || resourceID != schedule.ID {
		t.Fatalf("audit row = %q/%q/%q", action, outcome, resourceID)
	}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, TrafficReportTest{ScheduleID: schedule.ID, Actor: actor, At: now}); err == nil {
		t.Fatal("recording after commit must fail")
	}
}
