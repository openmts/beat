package trafficreport

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

func TestReportTestQueuesWithoutConsumingScheduledPeriod(t *testing.T) {
	fixture := newReportFixture(t)
	ctx := WithActor(t.Context(), fixture.actor)
	result, err := fixture.service.TestRun(ctx, fixture.schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivery.State != "queued" || !result.Delivery.DeliveredAt.IsZero() || len(result.Report.Nodes) != 1 || result.Report.Nodes[0].Used != 30 {
		t.Fatalf("result=%+v", result)
	}
	stored, err := fixture.store.Get(t.Context(), fixture.schedule.ID)
	if err != nil || stored.LastRunAt != nil || stored.LastPeriodKey != "" || !stored.NextRunAt.Equal(fixture.schedule.NextRunAt) {
		t.Fatal("test consumed schedule period")
	}
	var count int
	if err := fixture.database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='traffic_report.test_queued' AND request_id=?", fixture.actor.RequestID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("test was not audited")
	}
	first := fixture.queue.requests[0].SourceKey
	if _, err := fixture.service.TestRun(ctx, fixture.schedule.ID); !errors.Is(err, notificationqueue.ErrRateLimited) {
		t.Fatal("repeated test not limited")
	}
	fixture.service.now = func() time.Time { return fixture.now.Add(2 * time.Minute) }
	if _, err := fixture.service.TestRun(ctx, fixture.schedule.ID); err != nil {
		t.Fatal(err)
	}
	if fixture.queue.requests[1].SourceKey == first {
		t.Fatal("new operator test reused source identity")
	}
}

func TestReportTestFailureAndAuthorityBoundaries(t *testing.T) {
	for _, kind := range []string{"missing actor", "queue missing", "missing schedule", "closed store", "queue failure", "revoked session", "invalid time zone", "report failure", "oversize report"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newReportFixture(t)
			ctx := WithActor(t.Context(), fixture.actor)
			id := fixture.schedule.ID
			switch kind {
			case "missing actor":
				ctx = t.Context()
			case "queue missing":
				fixture.service.queue = nil
			case "missing schedule":
				id = "missing"
			case "closed store":
				if err := fixture.database.Close(); err != nil {
					t.Fatal(err)
				}
			case "queue failure":
				fixture.queue.afterMutation = errors.New("queue full")
			case "revoked session":
				fixture.queue.before = func() {
					if err := store.NewAdminStore(fixture.database.DB).RevokeSession(t.Context(), fixture.actor.SessionID, model.NowUTC()); err != nil {
						t.Fatal(err)
					}
				}
			case "invalid time zone":
				if _, err := fixture.database.DB.Exec("UPDATE traffic_report_schedules SET timezone='Missing/Zone' WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			case "report failure":
				fixture.service.metrics = reportMetrics{err: errors.New("MTS unavailable")}
			case "oversize report":
				fixture.service.nodes = reportNodes{values: []model.Node{{ID: "node", Name: strings.Repeat("x", notificationqueue.MaximumTextBytes)}}}
			}
			if _, err := fixture.service.TestRun(ctx, id); err == nil {
				t.Fatal("invalid test succeeded")
			}
			if len(fixture.queue.requests) != 0 {
				t.Fatal("failed test produced notification")
			}
		})
	}
}

func TestReportTestBudgetExpiresAndBoundsDistinctKeys(t *testing.T) {
	budget := reportTestBudget{last: make(map[string]time.Time)}
	now := model.NowUTC()
	for index := 0; index < maximumRecentReportTests; index++ {
		if !budget.allow(fmt.Sprint(index), now) {
			t.Fatal("budget exhausted early")
		}
	}
	if budget.allow("extra", now) {
		t.Fatal("unbounded distinct tests accepted")
	}
	if !budget.allow("extra", now.Add(reportTestInterval)) {
		t.Fatal("budget did not expire")
	}
}

func TestReportHistoryRetainsKnownLegacyOutcome(t *testing.T) {
	fixture := newReportFixture(t)
	if _, err := fixture.database.DB.Exec("UPDATE traffic_report_schedules SET last_period_key='old',last_delivery_state='success',last_delivery_at=? WHERE id=?", model.NowUTC(), fixture.schedule.ID); err != nil {
		t.Fatal(err)
	}
	schedules, err := fixture.service.List(t.Context())
	if err != nil || schedules[0].LastDelivery.State != "success" {
		t.Fatal("known historical outcome lost")
	}
	if _, err := fixture.database.DB.Exec("UPDATE traffic_report_schedules SET last_delivery_state='queued' WHERE id=?", fixture.schedule.ID); err != nil {
		t.Fatal(err)
	}
	values, err := fixture.service.List(t.Context())
	if err != nil || values[0].LastDelivery.State != "unknown" || values[0].LastDelivery.Delivered != 0 {
		t.Fatal("retained schedule cannot distinguish expired delivery history")
	}
}
