package trafficreport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

const maximumReportNodes = 1000

const reportBuildTimeout = 30 * time.Second

var ErrReportTooLarge = errors.New("traffic report exceeds notification limits; split the schedule into smaller node scopes")

func (service *Service) RunDue(ctx context.Context) error {
	now := service.now()
	schedules, err := service.schedules.ListDue(ctx, now)
	if err != nil {
		return fmt.Errorf("list due traffic report schedules: %w", err)
	}
	var failures error
	for _, schedule := range schedules {
		if err := service.runScheduled(ctx, schedule, now); err != nil {
			failures = errors.Join(failures, fmt.Errorf("run schedule %s: %w", schedule.ID, err))
		}
	}
	return failures
}

func (service *Service) runScheduled(ctx context.Context, schedule model.TrafficReportSchedule, now time.Time) error {
	if service.queue == nil {
		return notificationqueue.ErrQueueUnavailable
	}
	period, err := ReportPeriod(schedule, schedule.NextRunAt)
	if err != nil {
		return err
	}
	next, err := NextRun(schedule, schedule.NextRunAt)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, reportBuildTimeout)
	defer cancel()
	report, err := service.buildReport(ctx, schedule, period)
	if err != nil {
		return err
	}
	request, err := reportRequest(report, schedule)
	if err != nil {
		return err
	}
	completion := store.TrafficReportCompletion{Schedule: schedule, PeriodKey: period.Key, NextRun: next, RunAt: now}
	_, err = service.queue.Enqueue(ctx, request, func(transaction *sql.Tx) error {
		changed, err := service.schedules.CompletePeriodTx(ctx, transaction, completion)
		if err != nil {
			return err
		}
		if !changed {
			return notificationqueue.ErrSkipMutation
		}
		return nil
	})
	return err
}

func reportRequest(report model.TrafficReport, schedule model.TrafficReportSchedule) (notificationqueue.Request, error) {
	request, err := reportMessage(report)
	if err != nil {
		return request, err
	}
	request.UseSubscriptions = schedule.AllChannels
	if !schedule.AllChannels {
		request.ChannelIDs = append([]string(nil), schedule.ChannelIDs...)
	}
	return request, nil
}

func reportSourceKey(id, period string) string { return "traffic_report:" + id + ":" + period }

func (service *Service) hydrateDelivery(ctx context.Context, schedule *model.TrafficReportSchedule) error {
	if schedule.LastPeriodKey == "" {
		return nil
	}
	if service.queue == nil {
		return notificationqueue.ErrQueueUnavailable
	}
	receipt, err := service.queue.SourceReceipt(ctx, reportSourceKey(schedule.ID, schedule.LastPeriodKey))
	if errors.Is(err, notificationqueue.ErrNotFound) {
		if schedule.LastDelivery == nil || schedule.LastDelivery.State == model.TrafficReportDeliveryQueued {
			schedule.LastDelivery = &model.TrafficReportDeliveryStatus{State: model.TrafficReportDeliveryUnknown,
				Message: "delivery history is no longer available", QueuedAt: schedule.LastRunAt}
		}
		return nil
	}
	if err != nil {
		return err
	}
	status := receiptDeliveryStatus(receipt, schedule.LastRunAt)
	schedule.LastDelivery = &status
	return nil
}

func receiptDeliveryStatus(receipt notificationqueue.Receipt, queuedAt *time.Time) model.TrafficReportDeliveryStatus {
	counts := receipt.Counts
	total := counts.Pending + counts.Leased + counts.Succeeded + counts.Dead + counts.Cancelled
	status := model.TrafficReportDeliveryStatus{State: model.TrafficReportDeliveryQueued, Delivered: counts.Succeeded, Total: total, QueuedAt: queuedAt,
		Message: fmt.Sprintf("delivered to %d/%d channels", counts.Succeeded, total)}
	switch receipt.State {
	case "succeeded":
		status.State = model.TrafficReportDeliverySuccess
	case "dead", "cancelled":
		status.State = model.TrafficReportDeliveryFailed
		if counts.Succeeded > 0 {
			status.State = model.TrafficReportDeliveryPartial
		}
	case "not_routed":
		status.State = "not_routed"
		status.Message = "no enabled channels matched this report"
	default:
		status.Message = fmt.Sprintf("queued; delivered to %d/%d channels", counts.Succeeded, total)
	}
	return status
}
