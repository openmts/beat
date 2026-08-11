package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestTrafficReportScheduleStoreLifecycleAndClaim(t *testing.T) {
	database := setupTestDB(t)
	nodes := NewNodeStore(database.DB)
	node, err := nodes.UpsertNode(t.Context(), "report-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	channels := NewAlertChannelStore(database.DB)
	channel, err := channels.CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Report", ChannelType: "webhook", Config: `{"url":"https://example.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	nextRun := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	schedules := NewTrafficReportScheduleStore(database.DB)
	created, err := schedules.Create(t.Context(), &model.TrafficReportSchedule{
		Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC",
		SendHour: 8, Weekday: 1, MonthDay: 1, AllNodes: false, NodeIDs: []string{node.ID},
		AllChannels: false, ChannelIDs: []string{channel.ID}, Enabled: true,
		NextRunAt: nextRun,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	listed, err := schedules.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].NodeIDs[0] != node.ID || listed[0].ChannelIDs[0] != channel.ID {
		t.Fatalf("listed schedules = %#v, error = %v", listed, err)
	}
	due, err := schedules.ListDue(t.Context(), nextRun)
	if err != nil || len(due) != 1 {
		t.Fatalf("due schedules = %#v, error = %v", due, err)
	}
	created.Name = "Updated"
	created.AllNodes = true
	created.NodeIDs = nil
	created.AllChannels = true
	created.ChannelIDs = nil
	created.NextRunAt = nextRun
	updated, err := schedules.Update(t.Context(), created.ID, created)
	if err != nil || updated == nil || updated.Name != "Updated" || len(updated.NodeIDs) != 0 || len(updated.ChannelIDs) != 0 {
		t.Fatalf("updated schedule = %#v, error = %v", updated, err)
	}

	claimed, err := schedules.ClaimDue(
		t.Context(), created.ID, "daily:2026-07-29", nextRun.Add(24*time.Hour), nextRun,
	)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, error = %v", claimed, err)
	}
	claimed, err = schedules.ClaimDue(
		t.Context(), created.ID, "daily:2026-07-29", nextRun.Add(24*time.Hour), nextRun,
	)
	if err != nil || claimed {
		t.Fatalf("duplicate claim = %v, error = %v", claimed, err)
	}

	status := model.TrafficReportDeliveryStatus{
		State: model.TrafficReportDeliverySuccess, Message: "delivered to 1/1 channels",
		Delivered: 1, Total: 1, DeliveredAt: nextRun,
	}
	if err := schedules.CompleteRun(t.Context(), created.ID, nextRun, status); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	got, err := schedules.Get(t.Context(), created.ID)
	if err != nil || got == nil || got.LastDelivery == nil || got.LastDelivery.Delivered != 1 {
		t.Fatalf("completed schedule = %#v, error = %v", got, err)
	}
	if err := schedules.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}
	got, err = schedules.Get(t.Context(), created.ID)
	if err != nil || got != nil {
		t.Fatalf("deleted schedule = %#v, error = %v", got, err)
	}
}

func TestTrafficReportScheduleStoreErrorPaths(t *testing.T) {
	database := setupTestDB(t)
	schedules := NewTrafficReportScheduleStore(database.DB)
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	schedule := &model.TrafficReportSchedule{
		Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC",
		Weekday: 1, MonthDay: 1, AllNodes: true, AllChannels: true,
		NextRunAt: time.Now().UTC(),
	}
	if _, err := schedules.ListDue(t.Context(), time.Now()); err == nil {
		t.Fatal("ListDue() error = nil")
	}
	if _, err := schedules.Get(t.Context(), "missing"); err == nil {
		t.Fatal("Get() error = nil")
	}
	if _, err := schedules.Create(t.Context(), schedule); err == nil {
		t.Fatal("Create() error = nil")
	}
	if _, err := schedules.Update(t.Context(), "missing", schedule); err == nil {
		t.Fatal("Update() error = nil")
	}
	if err := schedules.Delete(t.Context(), "missing"); err == nil {
		t.Fatal("Delete() error = nil")
	}
	if _, err := schedules.ClaimDue(t.Context(), "missing", "period", time.Now(), time.Now()); err == nil {
		t.Fatal("ClaimDue() error = nil")
	}
	if err := schedules.CompleteRun(
		t.Context(), "missing", time.Now(), model.TrafficReportDeliveryStatus{},
	); err == nil {
		t.Fatal("CompleteRun() error = nil")
	}
}
