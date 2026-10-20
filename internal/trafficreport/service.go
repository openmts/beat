package trafficreport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
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
	CompletePeriodTx(context.Context, *sql.Tx, store.TrafficReportCompletion) (bool, error)
	RecordTestRunTx(context.Context, *sql.Tx, store.TrafficReportTest) error
}

type nodeStore interface {
	ListNodes(context.Context, string) ([]model.Node, error)
}

type metricsStore interface {
	QueryTrafficUsage(
		context.Context,
		string,
		time.Time,
		time.Time,
	) (model.TrafficTotals, error)
}

type Queue interface {
	notificationqueue.Enqueuer
	SourceReceipt(context.Context, string) (notificationqueue.Receipt, error)
}

type Config struct {
	Schedules scheduleStore
	Nodes     nodeStore
	Metrics   metricsStore
	Queue     Queue
}

type Service struct {
	schedules scheduleStore
	nodes     nodeStore
	metrics   metricsStore
	queue     Queue
	now       func() time.Time
	tests     reportTestBudget
}

func NewService(config Config) *Service {
	return &Service{schedules: config.Schedules, nodes: config.Nodes, metrics: config.Metrics, queue: config.Queue, now: model.NowUTC,
		tests: reportTestBudget{last: make(map[string]time.Time)}}
}

func (s *Service) List(ctx context.Context) ([]model.TrafficReportSchedule, error) {
	schedules, err := s.schedules.List(ctx)
	if err != nil {
		return nil, err
	}
	for index := range schedules {
		if err := s.hydrateDelivery(ctx, &schedules[index]); err != nil {
			return nil, err
		}
	}
	return schedules, nil
}

func (s *Service) Create(
	ctx context.Context,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	if schedule == nil {
		return nil, ErrInvalidSchedule
	}
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
	if schedule == nil {
		return nil, ErrInvalidSchedule
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
