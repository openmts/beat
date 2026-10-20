package trafficreport

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

const reportTestInterval = time.Minute

const maximumRecentReportTests = 128

type reportActorKey struct{}

func WithActor(ctx context.Context, actor store.EventMutationActor) context.Context {
	return context.WithValue(ctx, reportActorKey{}, actor)
}

type reportTestBudget struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (budget *reportTestBudget) allow(key string, now time.Time) bool {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	for id, at := range budget.last {
		if !now.Before(at.Add(reportTestInterval)) {
			delete(budget.last, id)
		}
	}
	if _, exists := budget.last[key]; exists || len(budget.last) >= maximumRecentReportTests {
		return false
	}
	budget.last[key] = now
	return true
}

func (service *Service) TestRun(ctx context.Context, id string) (model.TrafficReportRunResult, error) {
	input, err := service.testInput(ctx, id)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	schedule, now := input.Schedule, input.At
	runAt, err := PreviousRun(schedule, now)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	period, err := ReportPeriod(schedule, runAt)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, reportBuildTimeout)
	defer cancel()
	report, err := service.buildReport(ctx, schedule, period)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	request, err := reportRequest(report, schedule)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	request.SourceKey = "traffic_report_test:" + uuid.NewString()
	receipt, err := service.queue.Enqueue(ctx, request, func(transaction *sql.Tx) error {
		return service.schedules.RecordTestRunTx(ctx, transaction, input.Audit)
	})
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	return model.TrafficReportRunResult{Report: report, Delivery: receiptDeliveryStatus(receipt, &now)}, nil
}

type reportTestInput struct {
	Schedule model.TrafficReportSchedule
	At       time.Time
	Audit    store.TrafficReportTest
}

func (service *Service) testInput(ctx context.Context, id string) (reportTestInput, error) {
	actor, ok := ctx.Value(reportActorKey{}).(store.EventMutationActor)
	if !ok || actor.UserID == "" || actor.SessionID == "" {
		return reportTestInput{}, notificationqueue.ErrForbidden
	}
	if service.queue == nil {
		return reportTestInput{}, notificationqueue.ErrQueueUnavailable
	}
	schedule, err := service.schedules.Get(ctx, id)
	if err != nil {
		return reportTestInput{}, err
	}
	if schedule == nil {
		return reportTestInput{}, ErrScheduleNotFound
	}
	now := service.now()
	if !service.tests.allow(id, now) {
		return reportTestInput{}, notificationqueue.ErrRateLimited
	}
	return reportTestInput{Schedule: *schedule, At: now, Audit: store.TrafficReportTest{ScheduleID: id, Actor: actor, At: now}}, nil
}
