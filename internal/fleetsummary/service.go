package fleetsummary

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
	ErrInvalidSchedule  = errors.New("invalid fleet summary schedule")
	ErrScheduleNotFound = errors.New("fleet summary schedule not found")
	ErrConflict         = errors.New("fleet summary schedule changed")
	ErrSummaryTooLarge  = errors.New("fleet summary exceeds notification limits")
)

type scheduleStore interface {
	List(context.Context) ([]model.FleetSummarySchedule, error)
	ListDue(context.Context, time.Time) ([]model.FleetSummarySchedule, error)
	Get(context.Context, string) (*model.FleetSummarySchedule, error)
	Create(context.Context, *model.FleetSummarySchedule) (*model.FleetSummarySchedule, error)
	Update(context.Context, string, int, *model.FleetSummarySchedule) (*model.FleetSummarySchedule, error)
	Delete(context.Context, string, int) error
	CompleteRunTx(context.Context, *sql.Tx, store.FleetSummaryCompletion) (bool, error)
	RecordTestRunTx(context.Context, *sql.Tx, store.FleetSummaryTest) error
}

type nodeStore interface {
	ListNodes(context.Context, string) ([]model.Node, error)
}

type groupStore interface {
	ListGroups(context.Context) ([]model.Group, error)
}

// Queue 是持久通知 outbox 的写入口与回执读入口。
type Queue interface {
	notificationqueue.Enqueuer
	SourceReceipt(context.Context, string) (notificationqueue.Receipt, error)
}

type Config struct {
	Schedules scheduleStore
	Nodes     nodeStore
	Groups    groupStore
	Queue     Queue
}

type Service struct {
	schedules scheduleStore
	nodes     nodeStore
	groups    groupStore
	queue     Queue
	now       func() time.Time
	tests     testBudget
}

func NewService(config Config) *Service {
	return &Service{
		schedules: config.Schedules, nodes: config.Nodes, groups: config.Groups, queue: config.Queue,
		now: model.NowUTC, tests: testBudget{last: make(map[string]time.Time)},
	}
}

func (s *Service) List(ctx context.Context) ([]model.FleetSummarySchedule, error) {
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
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	if schedule == nil {
		return nil, ErrInvalidSchedule
	}
	normalizeSchedule(schedule)
	if err := ValidateSchedule(*schedule); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	next, err := NextRun(*schedule, s.now())
	if err != nil {
		return nil, err
	}
	schedule.NextRunAt = &next
	return s.schedules.Create(ctx, schedule)
}

func (s *Service) Update(
	ctx context.Context,
	id string,
	expectedRevision int,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	if schedule == nil {
		return nil, ErrInvalidSchedule
	}
	existing, err := s.schedules.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get fleet summary schedule: %w", err)
	}
	if existing == nil {
		return nil, ErrScheduleNotFound
	}
	normalizeSchedule(schedule)
	if err := ValidateSchedule(*schedule); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	next, err := NextRun(*schedule, s.now())
	if err != nil {
		return nil, err
	}
	schedule.NextRunAt = &next
	schedule.UpdatedAt = model.NowUTC()
	if expectedRevision <= 0 {
		expectedRevision = existing.Revision
	}
	updated, err := s.schedules.Update(ctx, id, expectedRevision, schedule)
	return updated, conflictError(err)
}

func (s *Service) Delete(ctx context.Context, id string, expectedRevision int) error {
	existing, err := s.schedules.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get fleet summary schedule: %w", err)
	}
	if existing == nil {
		return ErrScheduleNotFound
	}
	if expectedRevision <= 0 {
		expectedRevision = existing.Revision
	}
	return conflictError(s.schedules.Delete(ctx, id, expectedRevision))
}

// conflictError 把存储层的版本冲突翻译成服务层错误。
func conflictError(err error) error {
	if errors.Is(err, store.ErrFleetSummaryConflict) {
		return ErrConflict
	}
	return err
}

func normalizeSchedule(schedule *model.FleetSummarySchedule) {
	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	schedule.LocalTime = strings.TrimSpace(schedule.LocalTime)
	if schedule.Cadence != model.FleetSummaryWeekly {
		schedule.Weekday = nil
	} else if schedule.Weekday != nil {
		value := *schedule.Weekday
		schedule.Weekday = &value
	}
	schedule.NodeIDs = uniqueIDs(schedule.NodeIDs)
	schedule.GroupIDs = uniqueIDs(schedule.GroupIDs)
	schedule.ChannelIDs = uniqueIDs(schedule.ChannelIDs)
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, found := seen[id]; found {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
