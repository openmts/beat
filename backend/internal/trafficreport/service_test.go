package trafficreport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

type fakeScheduleStore struct {
	schedule      *model.TrafficReportSchedule
	claimCount    int
	completeCount int
	listErr       error
	dueErr        error
	getErr        error
	createErr     error
	updateErr     error
	deleteErr     error
	claimErr      error
	completeErr   error
}

func (f *fakeScheduleStore) List(context.Context) ([]model.TrafficReportSchedule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.schedule == nil {
		return []model.TrafficReportSchedule{}, nil
	}
	return []model.TrafficReportSchedule{*f.schedule}, nil
}

func (f *fakeScheduleStore) ListDue(context.Context, time.Time) ([]model.TrafficReportSchedule, error) {
	if f.dueErr != nil {
		return nil, f.dueErr
	}
	if f.schedule == nil {
		return []model.TrafficReportSchedule{}, nil
	}
	return []model.TrafficReportSchedule{*f.schedule}, nil
}

func (f *fakeScheduleStore) Get(_ context.Context, _ string) (*model.TrafficReportSchedule, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.schedule, nil
}

func (f *fakeScheduleStore) Create(
	_ context.Context,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	schedule.ID = "schedule"
	f.schedule = schedule
	return schedule, nil
}

func (f *fakeScheduleStore) Update(
	_ context.Context,
	_ string,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.schedule = schedule
	return schedule, nil
}

func (f *fakeScheduleStore) Delete(context.Context, string) error { return f.deleteErr }

func (f *fakeScheduleStore) ClaimDue(
	_ context.Context,
	_ string,
	_ string,
	_ time.Time,
	_ time.Time,
) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	f.claimCount++
	return f.claimCount == 1, nil
}

func (f *fakeScheduleStore) CompleteRun(
	_ context.Context,
	_ string,
	_ time.Time,
	status model.TrafficReportDeliveryStatus,
) error {
	if f.completeErr != nil {
		return f.completeErr
	}
	f.completeCount++
	f.schedule.LastDelivery = &status
	return nil
}

type fakeNodes struct {
	nodes []model.Node
	err   error
}

func (f fakeNodes) ListNodes(context.Context, string) ([]model.Node, error) { return f.nodes, f.err }

type fakeChannels struct {
	channels []model.AlertChannel
	err      error
}

func (f fakeChannels) ListAlertChannels(context.Context) ([]model.AlertChannel, error) {
	return f.channels, f.err
}

type fakeMetrics struct {
	totals model.TrafficTotals
	err    error
}

func (f fakeMetrics) QueryTrafficUsage(
	context.Context,
	string,
	time.Time,
	time.Time,
) (model.TrafficTotals, error) {
	return f.totals, f.err
}

type fakeDelivery struct {
	messages []notification.Message
	failID   string
}

func (f *fakeDelivery) SendMessage(
	_ context.Context,
	channel *model.AlertChannel,
	message notification.Message,
) (model.AlertDeliveryStatus, error) {
	f.messages = append(f.messages, message)
	status := model.AlertDeliveryStatus{State: notification.DeliverySuccess, DeliveredAt: model.NowUTC()}
	if channel.ID == f.failID {
		status.State = notification.DeliveryFailed
		return status, errors.New("delivery failed")
	}
	return status, nil
}

func TestServiceCreatesScheduleWithNextRun(t *testing.T) {
	store := &fakeScheduleStore{}
	service := NewService(store, fakeNodes{}, fakeChannels{}, fakeMetrics{}, &fakeDelivery{})
	service.now = func() time.Time { return time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC) }
	created, err := service.Create(t.Context(), &model.TrafficReportSchedule{
		Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC",
		SendHour: 8, AllNodes: true, AllChannels: true, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if created.Weekday != 1 || created.MonthDay != 1 ||
		!created.NextRunAt.Equal(time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("created schedule = %#v", created)
	}
}

func TestServiceRunsDueScheduleOnceAndBuildsMTSReport(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	schedule := reportSchedule(now)
	store := &fakeScheduleStore{schedule: &schedule}
	delivery := &fakeDelivery{}
	service := NewService(
		store,
		fakeNodes{nodes: []model.Node{{
			ID: "node", Name: "beat-host", TrafficLimitType: model.TrafficLimitSum,
			TrafficResetDay: 1,
		}}},
		fakeChannels{channels: []model.AlertChannel{{ID: "channel", Enabled: true}}},
		fakeMetrics{totals: model.TrafficTotals{Sent: 10, Received: 20}},
		delivery,
	)
	service.now = func() time.Time { return now }
	if err := service.RunDue(t.Context()); err != nil {
		t.Fatalf("run due: %v", err)
	}
	if err := service.RunDue(t.Context()); err != nil {
		t.Fatalf("run duplicate: %v", err)
	}
	if store.claimCount != 2 || store.completeCount != 1 || len(delivery.messages) != 1 {
		t.Fatalf("claims = %d, completions = %d, messages = %d", store.claimCount, store.completeCount, len(delivery.messages))
	}
	report, ok := delivery.messages[0].Data.(model.TrafficReport)
	if !ok || len(report.Nodes) != 1 || report.Nodes[0].Used != 30 {
		t.Fatalf("report = %#v", delivery.messages[0].Data)
	}
}

func TestServiceTestRunDoesNotClaimOrComplete(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule := reportSchedule(time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC))
	store := &fakeScheduleStore{schedule: &schedule}
	delivery := &fakeDelivery{}
	service := NewService(
		store, fakeNodes{}, fakeChannels{channels: []model.AlertChannel{{ID: "channel", Enabled: true}}},
		fakeMetrics{}, delivery,
	)
	service.now = func() time.Time { return now }
	result, err := service.TestRun(t.Context(), schedule.ID)
	if err != nil {
		t.Fatalf("test run: %v", err)
	}
	if store.claimCount != 0 || store.completeCount != 0 || result.Delivery.State != model.TrafficReportDeliverySuccess {
		t.Fatalf("result = %#v, claims = %d, completions = %d", result, store.claimCount, store.completeCount)
	}
}

func TestServiceRecordsPartialDelivery(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	schedule := reportSchedule(now)
	schedule.AllChannels = true
	store := &fakeScheduleStore{schedule: &schedule}
	delivery := &fakeDelivery{failID: "bad"}
	service := NewService(
		store, fakeNodes{}, fakeChannels{channels: []model.AlertChannel{
			{ID: "good", Enabled: true}, {ID: "bad", Enabled: true}, {ID: "off", Enabled: false},
		}}, fakeMetrics{}, delivery,
	)
	service.now = func() time.Time { return now }
	if err := service.RunDue(t.Context()); err == nil {
		t.Fatal("partial delivery error = nil")
	}
	if store.schedule.LastDelivery == nil || store.schedule.LastDelivery.State != model.TrafficReportDeliveryPartial ||
		store.schedule.LastDelivery.Delivered != 1 || store.schedule.LastDelivery.Total != 2 {
		t.Fatalf("delivery status = %#v", store.schedule.LastDelivery)
	}
}

func TestServiceCRUDValidationAndErrors(t *testing.T) {
	store := &fakeScheduleStore{}
	service := NewService(store, fakeNodes{}, fakeChannels{}, fakeMetrics{}, &fakeDelivery{})
	service.now = func() time.Time { return time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC) }
	if schedules, err := service.List(t.Context()); err != nil || len(schedules) != 0 {
		t.Fatalf("list schedules = %#v, error = %v", schedules, err)
	}
	invalid := &model.TrafficReportSchedule{Name: "", Cadence: "bad", Timezone: "UTC"}
	if _, err := service.Create(t.Context(), invalid); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("create error = %v", err)
	}
	created, err := service.Create(t.Context(), &model.TrafficReportSchedule{
		Name: " Explicit ", Cadence: model.TrafficReportWeekly, Timezone: "UTC",
		Weekday: 2, AllNodes: false, NodeIDs: []string{"node", "node", " "},
		AllChannels: false, ChannelIDs: []string{"channel", "channel"}, Enabled: true,
	})
	if err != nil || created.Name != "Explicit" || len(created.NodeIDs) != 1 || len(created.ChannelIDs) != 1 {
		t.Fatalf("created schedule = %#v, error = %v", created, err)
	}
	store.schedule = nil
	if _, err := service.Update(t.Context(), "missing", created); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("missing update error = %v", err)
	}
	if err := service.Delete(t.Context(), "missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("missing delete error = %v", err)
	}
	if _, err := service.TestRun(t.Context(), "missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("missing test error = %v", err)
	}
	store.schedule = created
	badUpdate := *created
	badUpdate.Timezone = "Missing/Zone"
	if _, err := service.Update(t.Context(), created.ID, &badUpdate); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid update error = %v", err)
	}
	goodUpdate := *created
	goodUpdate.Name = "Updated"
	if updated, err := service.Update(t.Context(), created.ID, &goodUpdate); err != nil || updated.Name != "Updated" {
		t.Fatalf("updated schedule = %#v, error = %v", updated, err)
	}
	if err := service.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}

	store.listErr = errors.New("list failed")
	if _, err := service.List(t.Context()); err == nil {
		t.Fatal("List() error = nil")
	}
	store.dueErr = errors.New("due failed")
	if err := service.RunDue(t.Context()); err == nil {
		t.Fatal("RunDue() error = nil")
	}
}

func TestServiceRunFailurePaths(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		store    *fakeScheduleStore
		nodes    fakeNodes
		channels fakeChannels
		metrics  fakeMetrics
	}{
		{name: "claim", store: &fakeScheduleStore{claimErr: errors.New("claim failed")}},
		{name: "nodes", store: &fakeScheduleStore{}, nodes: fakeNodes{err: errors.New("nodes failed")}},
		{name: "metrics", store: &fakeScheduleStore{}, nodes: fakeNodes{nodes: []model.Node{{ID: "node", TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1}}}, metrics: fakeMetrics{err: errors.New("mts failed")}},
		{name: "channels", store: &fakeScheduleStore{}, channels: fakeChannels{err: errors.New("channels failed")}},
		{name: "no channels", store: &fakeScheduleStore{}},
		{name: "complete", store: &fakeScheduleStore{completeErr: errors.New("complete failed")}, channels: fakeChannels{channels: []model.AlertChannel{{ID: "channel", Enabled: true}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedule := reportSchedule(now)
			test.store.schedule = &schedule
			service := NewService(test.store, test.nodes, test.channels, test.metrics, &fakeDelivery{})
			service.now = func() time.Time { return now }
			if err := service.RunDue(t.Context()); err == nil {
				t.Fatal("RunDue() error = nil")
			}
		})
	}
}

func TestReportSelectionAndFormattingHelpers(t *testing.T) {
	service := NewService(
		&fakeScheduleStore{},
		fakeNodes{nodes: []model.Node{
			{ID: "one", Name: "one", TrafficLimitType: model.TrafficLimitUp, TrafficResetDay: 1},
			{ID: "two", Name: "two", Alias: "Two", TrafficLimitType: model.TrafficLimitDown, TrafficResetDay: 1},
		}},
		fakeChannels{}, fakeMetrics{totals: model.TrafficTotals{Sent: 2048, Received: 1024}},
		&fakeDelivery{},
	)
	schedule := reportSchedule(time.Now())
	schedule.AllNodes = false
	schedule.NodeIDs = []string{"two"}
	period := model.TrafficReportPeriod{Start: time.Now().Add(-time.Hour), End: time.Now()}
	report, err := service.buildReport(t.Context(), schedule, period)
	if err != nil || len(report.Nodes) != 1 || report.Nodes[0].Name != "two" || report.Nodes[0].Used != 1024 {
		t.Fatalf("report = %#v, error = %v", report, err)
	}
	for _, cadence := range []string{model.TrafficReportDaily, model.TrafficReportWeekly, model.TrafficReportMonthly, "other"} {
		report.Cadence = cadence
		report.Timezone = "UTC"
		report.ScheduleName = "Report"
		report.Nodes = []model.TrafficReportNode{{Name: "one", Sent: 1}, {Name: "two", Alias: "Two", Sent: 2048}}
		message, err := reportMessage(report)
		if err != nil || message.Kind != "traffic_report" || message.Text == "" {
			t.Fatalf("message = %#v, error = %v", message, err)
		}
	}
	if _, err := reportMessage(model.TrafficReport{Timezone: "Missing/Zone"}); err == nil {
		t.Fatal("reportMessage() timezone error = nil")
	}
	if formatBytes(1) != "1 B" || formatBytes(1024) != "1.00 KiB" || formatBytes(1024*1024*1024*1024*1024*1024) == "" {
		t.Fatalf("unexpected byte formatting")
	}
}

func reportSchedule(nextRun time.Time) model.TrafficReportSchedule {
	return model.TrafficReportSchedule{
		ID: "schedule", Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC",
		SendHour: 8, Weekday: 1, MonthDay: 1, AllNodes: true,
		AllChannels: false, ChannelIDs: []string{"channel"}, Enabled: true, NextRunAt: nextRun,
	}
}
