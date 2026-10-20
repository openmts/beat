package trafficreport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func TestScheduledReportQueuesOnceWithAtomicCursor(t *testing.T) {
	fixture := newReportFixture(t)
	if err := fixture.service.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.runScheduled(t.Context(), fixture.schedule, fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(fixture.queue.requests) != 1 {
		t.Fatal("duplicate report was queued")
	}
	request := fixture.queue.requests[0]
	if request.SourceKey != "traffic_report:"+fixture.schedule.ID+":daily:2026-07-29" || !request.UseSubscriptions || !strings.Contains(request.Text, "edge: up 10 B, down 20 B, used 30 B") {
		t.Fatalf("request=%+v", request)
	}
	schedules, err := fixture.service.List(t.Context())
	if err != nil || len(schedules) != 1 {
		t.Fatal(err)
	}
	status := schedules[0].LastDelivery
	if status == nil || status.State != model.TrafficReportDeliveryQueued || !status.DeliveredAt.IsZero() || status.Delivered != 0 || status.QueuedAt == nil {
		t.Fatalf("enqueue falsely claims delivery: %+v", status)
	}
	if !schedules[0].NextRunAt.After(fixture.schedule.NextRunAt) {
		t.Fatal("cursor did not advance with queue")
	}
}

func TestReportQueueFailureAndConcurrentEditDoNotAdvanceCursor(t *testing.T) {
	for _, kind := range []string{"queue failure", "concurrent edit", "audit failure"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newReportFixture(t)
			switch kind {
			case "queue failure":
				fixture.queue.afterMutation = errors.New("queue unavailable")
			case "concurrent edit":
				fixture.queue.before = func() {
					if _, err := fixture.database.DB.Exec("UPDATE traffic_report_schedules SET enabled=0 WHERE id=?", fixture.schedule.ID); err != nil {
						t.Fatal(err)
					}
				}
			case "audit failure":
				if _, err := fixture.database.DB.Exec(`CREATE TRIGGER fail_report_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err := fixture.service.RunDue(t.Context())
			if kind != "concurrent edit" && err == nil {
				t.Fatal("persistence failure hidden")
			}
			stored, err := fixture.store.Get(t.Context(), fixture.schedule.ID)
			if err != nil || stored.LastPeriodKey != "" || !stored.NextRunAt.Equal(fixture.schedule.NextRunAt) || len(fixture.queue.requests) != 0 {
				t.Fatal("failed report advanced cursor or queued notification")
			}
		})
	}
}

func TestReportBuildFailureNeverClaimsThePeriod(t *testing.T) {
	for _, kind := range []string{"nodes", "metrics", "oversize text", "oversize scope", "invalid policy", "cancelled", "queue missing", "invalid timezone", "invalid cadence"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newReportFixture(t)
			ctx := t.Context()
			switch kind {
			case "nodes":
				fixture.service.nodes = reportNodes{err: errors.New("node database failed")}
			case "metrics":
				fixture.service.metrics = reportMetrics{err: errors.New("MTS failed")}
			case "oversize text":
				fixture.service.nodes = reportNodes{values: []model.Node{{ID: "node", Name: strings.Repeat("x", notificationqueue.MaximumTextBytes)}}}
			case "oversize scope":
				fixture.service.nodes = reportNodes{values: make([]model.Node, maximumReportNodes+1)}
			case "invalid policy":
				fixture.service.nodes = reportNodes{values: []model.Node{{ID: "node", TrafficLimitType: "bad"}}}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "queue missing":
				fixture.service.queue = nil
			case "invalid timezone":
				fixture.schedule.Timezone = "Invalid/Zone"
			case "invalid cadence":
				fixture.schedule.Cadence = "bad"
			}
			if err := fixture.service.runScheduled(ctx, fixture.schedule, fixture.now); err == nil {
				t.Fatal("failed build accepted")
			}
			stored, err := fixture.store.Get(t.Context(), fixture.schedule.ID)
			if err != nil || stored.LastPeriodKey != "" || len(fixture.queue.requests) != 0 {
				t.Fatal("build failure consumed report period")
			}
		})
	}
}

func TestReportListHydratesCurrentDeliveryOutcome(t *testing.T) {
	fixture := newReportFixture(t)
	if err := fixture.service.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := fixture.queue.requests[0].SourceKey
	for _, test := range []struct {
		state  string
		counts model.NotificationDeliveryCounts
		want   string
	}{
		{"pending", model.NotificationDeliveryCounts{Pending: 1, Succeeded: 1}, "queued"},
		{"leased", model.NotificationDeliveryCounts{Leased: 1}, "queued"},
		{"succeeded", model.NotificationDeliveryCounts{Succeeded: 2}, "success"},
		{"dead", model.NotificationDeliveryCounts{Dead: 1, Succeeded: 1}, "partial"},
		{"dead", model.NotificationDeliveryCounts{Dead: 2}, "failed"},
		{"cancelled", model.NotificationDeliveryCounts{Cancelled: 2}, "failed"},
		{"not_routed", model.NotificationDeliveryCounts{}, "not_routed"},
	} {
		fixture.queue.messages[key] = notificationqueue.Receipt{State: test.state, Counts: test.counts}
		schedules, err := fixture.service.List(t.Context())
		if err != nil || schedules[0].LastDelivery.State != test.want || schedules[0].LastDelivery.Delivered != test.counts.Succeeded {
			t.Fatalf("%s: %+v/%v", test.state, schedules, err)
		}
	}
	fixture.queue.readError = errors.New("queue read failed")
	if _, err := fixture.service.List(t.Context()); err == nil {
		t.Fatal("queue error hidden")
	}
	fixture.service.queue = nil
	if _, err := fixture.service.List(t.Context()); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
		t.Fatal(err)
	}
}

func TestReportFormattingPreservesSelectedNodes(t *testing.T) {
	fixture := newReportFixture(t)
	fixture.service.nodes = reportNodes{values: []model.Node{{ID: "one", Name: "hidden"}, {ID: "two", Name: "edge", Alias: "Office", TrafficLimitType: model.TrafficLimitDown}}}
	schedule := fixture.schedule
	schedule.AllNodes = false
	schedule.NodeIDs = []string{"two"}
	schedule.AllChannels = false
	schedule.ChannelIDs = []string{"channel"}
	period := model.TrafficReportPeriod{Start: fixture.now.Add(-time.Hour), End: fixture.now}
	report, err := fixture.service.buildReport(t.Context(), schedule, period)
	if err != nil || len(report.Nodes) != 1 || report.Nodes[0].Used != 20 {
		t.Fatal(err)
	}
	for _, cadence := range []string{model.TrafficReportDaily, model.TrafficReportWeekly, model.TrafficReportMonthly, "other"} {
		report.Cadence = cadence
		request, err := reportRequest(report, schedule)
		if err != nil || !strings.Contains(request.Text, "Office (edge)") || strings.Contains(request.Text, "hidden") || request.ChannelIDs[0] != "channel" || request.UseSubscriptions {
			t.Fatalf("request=%+v/%v", request, err)
		}
	}
	if _, err := reportMessage(model.TrafficReport{Timezone: "Missing/Zone"}); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	request, err := reportMessage(model.TrafficReport{Timezone: "UTC"})
	if err != nil || !strings.Contains(request.Text, "no nodes in scope") {
		t.Fatal("missing empty report state")
	}
	if formatBytes(1) != "1 B" || formatBytes(1024) != "1.00 KiB" || formatBytes(1<<60) != "1024.00 PiB" {
		t.Fatal("byte formatting failed")
	}
}
