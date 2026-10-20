package fleetsummary

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

const (
	summaryTestInterval       = time.Minute
	maximumRecentSummaryTests = 128
)

type actorKey struct{}

// WithActor 注入发起手动测试的管理员会话；测试发送不需要最近再认证。
func WithActor(ctx context.Context, actor store.EventMutationActor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

type testBudget struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (budget *testBudget) allow(key string, now time.Time) bool {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	for id, at := range budget.last {
		if !now.Before(at.Add(summaryTestInterval)) {
			delete(budget.last, id)
		}
	}
	if _, exists := budget.last[key]; exists || len(budget.last) >= maximumRecentSummaryTests {
		return false
	}
	budget.last[key] = now
	return true
}

// TestRun 使用当前可用性状态发送一次被标记为测试的摘要，不推进自动调度游标。
func (s *Service) TestRun(ctx context.Context, id string) (model.FleetSummaryRun, error) {
	actor, ok := ctx.Value(actorKey{}).(store.EventMutationActor)
	if !ok || actor.UserID == "" || actor.SessionID == "" {
		return model.FleetSummaryRun{}, notificationqueue.ErrForbidden
	}
	if s.queue == nil {
		return model.FleetSummaryRun{}, notificationqueue.ErrQueueUnavailable
	}
	schedule, err := s.schedules.Get(ctx, id)
	if err != nil {
		return model.FleetSummaryRun{}, err
	}
	if schedule == nil {
		return model.FleetSummaryRun{}, ErrScheduleNotFound
	}
	now := s.now()
	if !s.tests.allow(id, now) {
		return model.FleetSummaryRun{}, notificationqueue.ErrRateLimited
	}
	ctx, cancel := context.WithTimeout(ctx, summaryBuildTimeout)
	defer cancel()
	counts, groupNames, err := s.snapshot(ctx, *schedule)
	if err != nil {
		return model.FleetSummaryRun{}, err
	}
	periodKey := "test:" + uuid.NewString()
	request, err := summaryRequest(*schedule, now, periodKey, counts, groupNames)
	if err != nil {
		return model.FleetSummaryRun{}, err
	}
	run := model.FleetSummaryRun{
		ScheduleID: id, ScheduleName: schedule.Name, Cadence: schedule.Cadence, Timezone: schedule.Timezone,
		PeriodKey: periodKey, Cutoff: now, Kind: model.FleetSummaryRunTest,
		Status: model.FleetSummaryResultQueued, Counts: counts,
		ChannelCount: len(schedule.ChannelIDs), CreatedAt: now,
	}
	_, err = s.queue.Enqueue(ctx, request, func(transaction *sql.Tx) error {
		return s.schedules.RecordTestRunTx(ctx, transaction, store.FleetSummaryTest{
			ScheduleID: id, Actor: actor, At: now, Run: run,
		})
	})
	if err != nil {
		return model.FleetSummaryRun{}, err
	}
	return run, nil
}
