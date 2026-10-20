package fleetsummary

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

func TestScheduleHelpersRejectInvalidInput(t *testing.T) {
	due := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	daily := dailySchedule("UTC", "08:00")
	for _, test := range []struct {
		name, periodKey string
		schedule        model.FleetSummarySchedule
	}{
		{name: "unknown timezone", periodKey: "timezone", schedule: model.FleetSummarySchedule{Cadence: model.FleetSummaryDaily, Timezone: "Not/AZone", LocalTime: "08:00"}},
		{name: "missing colon", schedule: model.FleetSummarySchedule{Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "0800"}},
		{name: "non numeric hour", schedule: model.FleetSummarySchedule{Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "aa:00"}},
		{name: "unknown cadence", periodKey: "cadence", schedule: model.FleetSummarySchedule{Cadence: "monthly", Timezone: "UTC", LocalTime: "08:00"}},
	} {
		if _, err := NextRun(test.schedule, due); err == nil {
			t.Fatalf("%s: NextRun accepted an invalid schedule", test.name)
		}
		if _, err := PeriodKey(test.schedule, due); (err == nil) == (test.periodKey != "") {
			t.Fatalf("%s: PeriodKey error = %v", test.name, err)
		}
	}
	if _, err := NextRun(daily, due); err != nil {
		t.Fatalf("daily schedule rejected: %v", err)
	}
	weekday := int(time.Monday)
	weekly := daily
	weekly.Cadence, weekly.Weekday = model.FleetSummaryWeekly, &weekday
	if key, err := PeriodKey(weekly, due); err != nil || !strings.Contains(key, "W") {
		t.Fatalf("weekly period key = %q, error = %v", key, err)
	}
}

func TestServiceSurfacesStoreFailures(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	failure := errors.New("store unavailable")

	listing := newTestService(&fakeStore{listErr: failure}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if _, err := listing.List(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("list error = %v", err)
	}

	schedule, schedules, queue := dueSummaryFixture(now)
	queue.receiptErr = failure
	schedule.LastPeriodKey = "2026-07-29"
	schedules.schedules[0] = schedule
	service := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	if _, err := service.List(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("delivery error = %v", err)
	}

	if _, err := service.Create(t.Context(), nil); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("nil create error = %v", err)
	}
	if _, err := service.Update(t.Context(), "existing", 0, nil); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("nil update error = %v", err)
	}
	if _, err := service.Update(t.Context(), schedule.ID, 0, &model.FleetSummarySchedule{Name: "Bad"}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid update error = %v", err)
	}

	broken := &fakeStore{getErr: failure}
	brokenService := newTestService(broken, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if _, err := brokenService.Update(t.Context(), "existing", 0, &schedule); !errors.Is(err, failure) {
		t.Fatalf("update get error = %v", err)
	}
	if err := brokenService.Delete(t.Context(), "existing", 0); !errors.Is(err, failure) {
		t.Fatalf("delete get error = %v", err)
	}
	if err := brokenService.Delete(t.Context(), "missing", 0); !errors.Is(err, failure) {
		t.Fatalf("delete of unknown schedule error = %v", err)
	}
}

func TestDeleteUsesRevisionAndTranslatesConflicts(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedules := &fakeStore{schedules: []model.FleetSummarySchedule{{ID: "existing", Revision: 4, Name: "Old"}}}
	service := newTestService(schedules, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if err := service.Delete(t.Context(), "existing", 0); err != nil || schedules.lastDeleteRevision != 4 {
		t.Fatalf("delete revision = %d, error = %v", schedules.lastDeleteRevision, err)
	}
	if err := service.Delete(t.Context(), "existing", 7); err != nil || schedules.lastDeleteRevision != 7 {
		t.Fatalf("explicit revision = %d, error = %v", schedules.lastDeleteRevision, err)
	}
	schedules.deleteErr = store.ErrFleetSummaryConflict
	if err := service.Delete(t.Context(), "existing", 7); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	schedules.deleteErr = errors.New("delete failed")
	if err := service.Delete(t.Context(), "existing", 7); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("unexpected error translation: %v", err)
	}
}

func TestNormalizeScheduleDropsDailyWeekdayAndDeduplicatesScope(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	service := newTestService(&fakeStore{}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	weekday := int(time.Sunday)
	weekly := dailySchedule(" UTC ", " 08:00 ")
	weekly.Cadence, weekly.Weekday = model.FleetSummaryWeekly, &weekday
	weekly.NodeIDs = []string{" one ", "", "one", "two"}
	weekly.GroupIDs = []string{" group ", "group"}
	weekly.ChannelIDs = []string{"channel", "channel", ""}
	created, err := service.Create(t.Context(), &weekly)
	if err != nil {
		t.Fatalf("create weekly schedule: %v", err)
	}
	if created.Weekday == nil || *created.Weekday != weekday || created.Timezone != "UTC" || created.LocalTime != "08:00" {
		t.Fatalf("normalized schedule = %+v", created)
	}
	if len(created.NodeIDs) != 2 || len(created.GroupIDs) != 1 || len(created.ChannelIDs) != 1 {
		t.Fatalf("deduplicated scope = %+v %+v %+v", created.NodeIDs, created.GroupIDs, created.ChannelIDs)
	}
	daily := dailySchedule("UTC", "08:00")
	daily.Weekday = &weekday
	if _, err := service.Create(t.Context(), &daily); err != nil {
		t.Fatalf("create daily schedule: %v", err)
	}
}

func TestRunDueReportsSchedulingFailuresPerSchedule(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	failure := errors.New("list due failed")
	if err := newTestService(&fakeStore{listDueErr: failure}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now).
		RunDue(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("list due error = %v", err)
	}
	due := now.Add(-time.Hour)
	queueless := newTestService(&fakeStore{dueOverride: []model.FleetSummarySchedule{{
		ID: "queueless", Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "08:00", ChannelIDs: []string{"channel"},
	}}}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	queueless.queue = nil
	if err := queueless.RunDue(t.Context()); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
		t.Fatalf("nil queue error = %v", err)
	}

	missingCursor := newTestService(&fakeStore{dueOverride: []model.FleetSummarySchedule{{
		ID: "no-cursor", Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "08:00", ChannelIDs: []string{"channel"},
	}}}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if err := missingCursor.RunDue(t.Context()); err == nil {
		t.Fatal("schedule without a cursor was accepted")
	}

	unknownCadence := newTestService(&fakeStore{dueOverride: []model.FleetSummarySchedule{{
		ID: "odd", Name: "Morning", Cadence: "monthly", Timezone: "UTC", LocalTime: "08:00", ChannelIDs: []string{"channel"}, NextRunAt: &due,
	}}}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if err := unknownCadence.RunDue(t.Context()); err == nil {
		t.Fatal("unknown cadence was accepted")
	}

	brokenLocalTime := newTestService(&fakeStore{dueOverride: []model.FleetSummarySchedule{{
		ID: "odd", Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "0800", ChannelIDs: []string{"channel"}, NextRunAt: &due,
	}}}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if err := brokenLocalTime.RunDue(t.Context()); err == nil {
		t.Fatal("invalid local time was accepted")
	}

	future := &fakeStore{dueOverride: []model.FleetSummarySchedule{{
		ID: "future", Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "08:00", ChannelIDs: []string{"channel"},
		NextRunAt: pointerTo(now.Add(48 * time.Hour)),
	}}, claim: true}
	advanced := newTestService(future, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	if err := advanced.RunDue(t.Context()); err != nil {
		t.Fatalf("future cursor run: %v", err)
	}
	if len(future.completions) != 1 || !future.completions[0].NextRun.After(*future.dueOverride[0].NextRunAt) {
		t.Fatalf("cursor did not advance past a future due time: %+v", future.completions)
	}
}

func TestRunDueReportsEnqueueAndGroupFailures(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	queue.enqueueErr = errors.New("queue failed")
	if err := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now).RunDue(t.Context()); err == nil {
		t.Fatal("enqueue failure was ignored")
	}

	schedules.claim = true
	schedules.completeErr = errors.New("completion failed")
	if err := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now).RunDue(t.Context()); err == nil {
		t.Fatal("completion failure was ignored")
	}

	schedules.completeErr = nil
	groupFailure := errors.New("groups failed")
	schedule.GroupIDs = []string{"group"}
	schedules.schedules[0] = schedule
	if err := newTestService(schedules, fakeNodes{}, fakeGroups{err: groupFailure}, queue, now).RunDue(t.Context()); !errors.Is(err, groupFailure) {
		t.Fatalf("group failure = %v", err)
	}
}

func TestSummaryRequestRejectsOversizedText(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	schedule.NameThreshold = model.FleetSummaryMaximumNameThreshold
	schedules.schedules[0] = schedule
	long := strings.Repeat("n", 200)
	nodes := fakeNodes{}
	for index := range model.FleetSummaryMaximumNameThreshold {
		nodes.nodes = append(nodes.nodes, model.Node{ID: string(rune('a' + index)), Name: long, Status: model.NodeStatusOnline})
		nodes.nodes = append(nodes.nodes, model.Node{ID: string(rune('A' + index)), Name: long, Status: model.NodeStatusOffline})
	}
	if err := newTestService(schedules, nodes, fakeGroups{}, queue, now).RunDue(t.Context()); !errors.Is(err, ErrSummaryTooLarge) {
		t.Fatalf("oversized summary error = %v", err)
	}
}

func TestListWithoutDeliveryCursorOnlyLoadsSchedules(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	_, schedules, queue := dueSummaryFixture(now)
	schedules.schedules[0].NextRunAt = nil
	listing := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	listed, err := listing.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].LastResult != nil {
		t.Fatalf("listed = %#v, error = %v", listed, err)
	}
}

func TestTestRunSurfacesStoreAndQueueFailures(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	actor := store.EventMutationActor{UserID: "owner", SessionID: "session"}
	ctx := WithActor(t.Context(), actor)

	queueless := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	queueless.queue = nil
	if _, err := queueless.TestRun(ctx, schedule.ID); !errors.Is(err, notificationqueue.ErrQueueUnavailable) {
		t.Fatalf("missing queue error = %v", err)
	}

	failure := errors.New("store failed")
	schedules.getErr = failure
	if _, err := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now).TestRun(ctx, schedule.ID); !errors.Is(err, failure) {
		t.Fatalf("lookup error = %v", err)
	}
	schedules.getErr = nil
	if _, err := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now).TestRun(ctx, "missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("unknown schedule error = %v", err)
	}
	if _, err := newTestService(schedules, fakeNodes{err: failure}, fakeGroups{}, queue, now).TestRun(ctx, schedule.ID); !errors.Is(err, failure) {
		t.Fatalf("snapshot error = %v", err)
	}

	oversized, oversizedSchedules, oversizedQueue := dueSummaryFixture(now)
	oversized.NameThreshold = model.FleetSummaryMaximumNameThreshold
	oversizedSchedules.schedules[0] = oversized
	long := strings.Repeat("n", 200)
	nodes := fakeNodes{}
	for index := range model.FleetSummaryMaximumNameThreshold {
		nodes.nodes = append(nodes.nodes, model.Node{ID: string(rune('a' + index)), Name: long, Status: model.NodeStatusOnline})
		nodes.nodes = append(nodes.nodes, model.Node{ID: string(rune('A' + index)), Name: long, Status: model.NodeStatusOffline})
	}
	if _, err := newTestService(oversizedSchedules, nodes, fakeGroups{}, oversizedQueue, now).TestRun(ctx, oversized.ID); !errors.Is(err, ErrSummaryTooLarge) {
		t.Fatalf("oversized test run error = %v", err)
	}

	nodes = fakeNodes{}
	drainSchedules := &fakeStore{schedules: []model.FleetSummarySchedule{schedule}}
	queue.enqueueErr = errors.New("enqueue failed")
	if _, err := newTestService(drainSchedules, nodes, fakeGroups{}, queue, now).TestRun(ctx, schedule.ID); err == nil {
		t.Fatal("enqueue failure was ignored")
	}
}

func TestTestBudgetEvictsExpiredEntries(t *testing.T) {
	budget := testBudget{last: map[string]time.Time{}}
	start := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	if !budget.allow("first", start) {
		t.Fatal("first request was rejected")
	}
	later := start.Add(2 * summaryTestInterval)
	if !budget.allow("second", later) {
		t.Fatal("second request was rejected")
	}
	if !budget.allow("first", later) {
		t.Fatal("expired budget entry was not evicted")
	}
}

func pointerTo(value time.Time) *time.Time { return &value }
