package trafficreport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

var (
	ErrInvalidSchedule  = errors.New("invalid traffic report schedule")
	ErrScheduleNotFound = errors.New("traffic report schedule not found")
)

type scheduleStore interface {
	List(context.Context) ([]model.TrafficReportSchedule, error)
	ListDue(context.Context, time.Time) ([]model.TrafficReportSchedule, error)
	Get(context.Context, string) (*model.TrafficReportSchedule, error)
	Create(context.Context, *model.TrafficReportSchedule) (*model.TrafficReportSchedule, error)
	Update(context.Context, string, *model.TrafficReportSchedule) (*model.TrafficReportSchedule, error)
	Delete(context.Context, string) error
	ClaimDue(context.Context, string, string, time.Time, time.Time) (bool, error)
	CompleteRun(context.Context, string, time.Time, model.TrafficReportDeliveryStatus) error
}

type nodeStore interface {
	ListNodes(context.Context, string) ([]model.Node, error)
}

type channelStore interface {
	ListAlertChannels(context.Context) ([]model.AlertChannel, error)
}

type metricsStore interface {
	QueryTrafficUsage(
		context.Context,
		string,
		time.Time,
		time.Time,
	) (model.TrafficTotals, error)
}

type deliveryService interface {
	SendMessage(
		context.Context,
		*model.AlertChannel,
		notification.Message,
	) (model.AlertDeliveryStatus, error)
}

type Service struct {
	schedules scheduleStore
	nodes     nodeStore
	channels  channelStore
	metrics   metricsStore
	delivery  deliveryService
	now       func() time.Time
}

func NewService(
	schedules scheduleStore,
	nodes nodeStore,
	channels channelStore,
	metrics metricsStore,
	delivery deliveryService,
) *Service {
	return &Service{
		schedules: schedules, nodes: nodes, channels: channels,
		metrics: metrics, delivery: delivery, now: model.NowUTC,
	}
}

func (s *Service) List(ctx context.Context) ([]model.TrafficReportSchedule, error) {
	return s.schedules.List(ctx)
}

func (s *Service) Create(
	ctx context.Context,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	normalizeSchedule(schedule)
	if err := ValidateSchedule(*schedule); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	nextRun, err := NextRun(*schedule, s.now())
	if err != nil {
		return nil, err
	}
	schedule.NextRunAt = nextRun
	return s.schedules.Create(ctx, schedule)
}

func (s *Service) Update(
	ctx context.Context,
	id string,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	existing, err := s.schedules.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get traffic report schedule: %w", err)
	}
	if existing == nil {
		return nil, ErrScheduleNotFound
	}
	normalizeSchedule(schedule)
	if err := ValidateSchedule(*schedule); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	schedule.NextRunAt, err = NextRun(*schedule, s.now())
	if err != nil {
		return nil, err
	}
	return s.schedules.Update(ctx, id, schedule)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	existing, err := s.schedules.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get traffic report schedule: %w", err)
	}
	if existing == nil {
		return ErrScheduleNotFound
	}
	return s.schedules.Delete(ctx, id)
}

func (s *Service) TestRun(
	ctx context.Context,
	id string,
) (model.TrafficReportRunResult, error) {
	schedule, err := s.schedules.Get(ctx, id)
	if err != nil {
		return model.TrafficReportRunResult{}, fmt.Errorf("get traffic report schedule: %w", err)
	}
	if schedule == nil {
		return model.TrafficReportRunResult{}, ErrScheduleNotFound
	}
	runAt, err := PreviousRun(*schedule, s.now())
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	return s.runUnclaimed(ctx, *schedule, runAt)
}

func (s *Service) RunDue(ctx context.Context) error {
	now := s.now()
	schedules, err := s.schedules.ListDue(ctx, now)
	if err != nil {
		return fmt.Errorf("list due traffic report schedules: %w", err)
	}
	errorsFound := make([]error, 0)
	for _, schedule := range schedules {
		if err := s.runScheduled(ctx, schedule, now); err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("run schedule %s: %w", schedule.ID, err))
		}
	}
	return errors.Join(errorsFound...)
}

func (s *Service) runScheduled(
	ctx context.Context,
	schedule model.TrafficReportSchedule,
	now time.Time,
) error {
	period, err := ReportPeriod(schedule, schedule.NextRunAt)
	if err != nil {
		return err
	}
	nextRun, err := NextRun(schedule, schedule.NextRunAt)
	if err != nil {
		return err
	}
	claimed, err := s.schedules.ClaimDue(ctx, schedule.ID, period.Key, nextRun, now)
	if err != nil || !claimed {
		return err
	}
	result, runErr := s.runForPeriod(ctx, schedule, period)
	if err := s.schedules.CompleteRun(ctx, schedule.ID, now, result.Delivery); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

func (s *Service) runUnclaimed(
	ctx context.Context,
	schedule model.TrafficReportSchedule,
	runAt time.Time,
) (model.TrafficReportRunResult, error) {
	period, err := ReportPeriod(schedule, runAt)
	if err != nil {
		return model.TrafficReportRunResult{}, err
	}
	return s.runForPeriod(ctx, schedule, period)
}

func (s *Service) runForPeriod(
	ctx context.Context,
	schedule model.TrafficReportSchedule,
	period model.TrafficReportPeriod,
) (model.TrafficReportRunResult, error) {
	report, err := s.buildReport(ctx, schedule, period)
	if err != nil {
		status := failedDeliveryStatus(s.now(), "failed to build traffic report")
		return model.TrafficReportRunResult{Delivery: status}, err
	}
	status, deliveryErr := s.deliver(ctx, schedule, report)
	return model.TrafficReportRunResult{Report: report, Delivery: status}, deliveryErr
}

func normalizeSchedule(schedule *model.TrafficReportSchedule) {
	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	if schedule.Weekday == 0 {
		schedule.Weekday = 1
	}
	if schedule.MonthDay == 0 {
		schedule.MonthDay = 1
	}
	schedule.NodeIDs = uniqueStrings(schedule.NodeIDs)
	schedule.ChannelIDs = uniqueStrings(schedule.ChannelIDs)
	if schedule.AllNodes {
		schedule.NodeIDs = []string{}
	}
	if schedule.AllChannels {
		schedule.ChannelIDs = []string{}
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func failedDeliveryStatus(now time.Time, message string) model.TrafficReportDeliveryStatus {
	return model.TrafficReportDeliveryStatus{
		State: model.TrafficReportDeliveryFailed, Message: message, DeliveredAt: now,
	}
}
