package fleetsummary

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type fakeStore struct {
	schedules          []model.FleetSummarySchedule
	dueOverride        []model.FleetSummarySchedule
	claim              bool
	completions        []store.FleetSummaryCompletion
	tests              []store.FleetSummaryTest
	listErr            error
	listDueErr         error
	getErr             error
	deleteErr          error
	completeErr        error
	lastDeleteRevision int
}

func (f *fakeStore) List(context.Context) ([]model.FleetSummarySchedule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.schedules, nil
}

func (f *fakeStore) ListDue(_ context.Context, now time.Time) ([]model.FleetSummarySchedule, error) {
	if f.listDueErr != nil {
		return nil, f.listDueErr
	}
	if f.dueOverride != nil {
		return f.dueOverride, nil
	}
	due := []model.FleetSummarySchedule{}
	for _, schedule := range f.schedules {
		if schedule.Enabled && schedule.NextRunAt != nil && !schedule.NextRunAt.After(now) {
			due = append(due, schedule)
		}
	}
	return due, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*model.FleetSummarySchedule, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for index := range f.schedules {
		if f.schedules[index].ID == id {
			return &f.schedules[index], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) Create(_ context.Context, schedule *model.FleetSummarySchedule) (*model.FleetSummarySchedule, error) {
	schedule.ID, schedule.Revision = "created", 1
	return schedule, nil
}

func (f *fakeStore) Update(
	_ context.Context,
	_ string,
	expectedRevision int,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	schedule.Revision = expectedRevision + 1
	return schedule, nil
}

func (f *fakeStore) Delete(_ context.Context, _ string, expectedRevision int) error {
	f.lastDeleteRevision = expectedRevision
	return f.deleteErr
}

func (f *fakeStore) CompleteRunTx(
	_ context.Context,
	_ *sql.Tx,
	completion store.FleetSummaryCompletion,
) (bool, error) {
	if f.completeErr != nil {
		return false, f.completeErr
	}
	f.completions = append(f.completions, completion)
	return f.claim, nil
}

func (f *fakeStore) RecordTestRunTx(_ context.Context, _ *sql.Tx, test store.FleetSummaryTest) error {
	f.tests = append(f.tests, test)
	return nil
}

type fakeQueue struct {
	requests   []notificationqueue.Request
	receipts   map[string]notificationqueue.Receipt
	receiptErr error
	enqueueErr error
}

func (q *fakeQueue) Enqueue(
	_ context.Context,
	request notificationqueue.Request,
	mutation func(*sql.Tx) error,
) (notificationqueue.Receipt, error) {
	if q.enqueueErr != nil {
		return notificationqueue.Receipt{}, q.enqueueErr
	}
	if mutation != nil {
		err := mutation(nil)
		if errors.Is(err, notificationqueue.ErrSkipMutation) {
			return notificationqueue.Receipt{Skipped: true}, nil
		}
		if err != nil {
			return notificationqueue.Receipt{}, err
		}
	}
	q.requests = append(q.requests, request)
	return notificationqueue.Receipt{State: "pending"}, nil
}

func (q *fakeQueue) SourceReceipt(_ context.Context, key string) (notificationqueue.Receipt, error) {
	if q.receiptErr != nil {
		return notificationqueue.Receipt{}, q.receiptErr
	}
	receipt, found := q.receipts[key]
	if !found {
		return notificationqueue.Receipt{}, notificationqueue.ErrNotFound
	}
	return receipt, nil
}

type fakeNodes struct {
	nodes []model.Node
	err   error
}

func (f fakeNodes) ListNodes(context.Context, string) ([]model.Node, error) {
	return f.nodes, f.err
}

type fakeGroups struct {
	groups []model.Group
	err    error
}

func (f fakeGroups) ListGroups(context.Context) ([]model.Group, error) { return f.groups, f.err }

func dueSummaryFixture(now time.Time) (model.FleetSummarySchedule, *fakeStore, *fakeQueue) {
	due := now.Add(-72 * time.Hour)
	schedule := model.FleetSummarySchedule{
		ID: "schedule", Revision: 1, Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC",
		LocalTime: "08:00", NameThreshold: 10, Enabled: true, ChannelIDs: []string{"channel"},
		NodeIDs: []string{}, GroupIDs: []string{}, NextRunAt: &due, UpdatedAt: now.Add(-96 * time.Hour),
	}
	schedules := &fakeStore{schedules: []model.FleetSummarySchedule{schedule}, claim: true}
	return schedule, schedules, &fakeQueue{receipts: map[string]notificationqueue.Receipt{}}
}

func newTestService(
	schedules *fakeStore,
	nodes fakeNodes,
	groups fakeGroups,
	queue *fakeQueue,
	now time.Time,
) *Service {
	service := NewService(Config{Schedules: schedules, Nodes: nodes, Groups: groups, Queue: queue})
	service.now = func() time.Time { return now }
	return service
}

func TestRunDueClaimsSingleCatchUpPeriodAndAdvancesCursor(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	nodes := fakeNodes{nodes: []model.Node{
		{ID: "one", Name: "edge-one", Alias: "Edge One", GroupID: "group", Status: model.NodeStatusOnline},
		{ID: "two", Name: "edge-two", GroupID: "other", Status: model.NodeStatusOffline},
	}}
	service := newTestService(schedules, nodes, fakeGroups{}, queue, now)
	if err := service.RunDue(t.Context()); err != nil {
		t.Fatalf("run due: %v", err)
	}
	if len(schedules.completions) != 1 {
		t.Fatalf("completions = %#v", schedules.completions)
	}
	completion := schedules.completions[0]
	if completion.PeriodKey != "2026-07-27" || !completion.NextRun.Equal(time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("completion = %#v", completion)
	}
	if completion.Run.Counts.Online != 1 || completion.Run.Counts.Offline != 1 || completion.Run.Counts.Total != 2 {
		t.Fatalf("counts = %#v", completion.Run.Counts)
	}
	if len(queue.requests) != 1 {
		t.Fatalf("requests = %#v", queue.requests)
	}
	request := queue.requests[0]
	if request.SourceKey != "fleet_summary:"+schedule.ID+":2026-07-27" || request.EventKind != "fleet.status_summary" {
		t.Fatalf("request = %#v", request)
	}
	if len(request.ChannelIDs) != 1 || request.ChannelIDs[0] != "channel" {
		t.Fatalf("channel scope = %#v", request.ChannelIDs)
	}
	if request.Data["fleet.online_names"] != "Edge One" || request.Data["fleet.offline_names"] != "edge-two" {
		t.Fatalf("payload data = %#v", request.Data)
	}
	if request.Data["fleet.total_count"] != "2" || request.Text == "" {
		t.Fatalf("payload data = %#v", request.Data)
	}
}

func TestRunDueSkipsClaimedPeriodAndReportsSnapshotFailures(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	_, schedules, queue := dueSummaryFixture(now)
	schedules.claim = false
	service := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	if err := service.RunDue(t.Context()); err != nil {
		t.Fatalf("skip result = %v", err)
	}
	if len(queue.requests) != 0 {
		t.Fatalf("requests = %#v", queue.requests)
	}

	failure := errors.New("database unavailable")
	failing := newTestService(schedules, fakeNodes{err: failure}, fakeGroups{}, queue, now)
	if err := failing.RunDue(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("snapshot failure = %v", err)
	}
}

func TestScopedSnapshotSelectsGroupsNodesAndPrivateNodes(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	schedule.NameThreshold = 0
	schedule.NodeIDs = []string{"direct"}
	schedule.GroupIDs = []string{"group"}
	schedules.schedules[0] = schedule
	nodes := fakeNodes{nodes: []model.Node{
		{ID: "direct", Name: "direct-node", GroupID: "other", Status: model.NodeStatusOnline, IsPublic: false},
		{ID: "grouped", Name: "grouped-node", GroupID: "group", Status: model.NodeStatusOnline},
		{ID: "excluded", Name: "excluded-node", GroupID: "elsewhere", Status: model.NodeStatusOffline},
	}}
	groups := fakeGroups{groups: []model.Group{{ID: "group", Name: "Production"}}}
	service := newTestService(schedules, nodes, groups, queue, now)
	if err := service.RunDue(t.Context()); err != nil {
		t.Fatalf("run due: %v", err)
	}
	counts := schedules.completions[0].Run.Counts
	if counts.Total != 2 || counts.Online != 2 || len(counts.OnlineNames) != 2 {
		t.Fatalf("scoped counts = %#v", counts)
	}
	if queue.requests[0].Data["fleet.online_names"] != "2 nodes (names omitted)" {
		t.Fatalf("thresholded names = %q", queue.requests[0].Data["fleet.online_names"])
	}
	if queue.requests[0].Data["fleet.group_names"] != "Production" {
		t.Fatalf("group names = %q", queue.requests[0].Data["fleet.group_names"])
	}
}

func TestCreateAndUpdateScheduleValidation(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	service := newTestService(&fakeStore{}, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	schedule := dailySchedule("UTC", "08:00")
	created, err := service.Create(t.Context(), &schedule)
	if err != nil || created.NextRunAt == nil || !created.NextRunAt.Equal(time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("created = %#v, error = %v", created, err)
	}
	if _, err := service.Create(t.Context(), &model.FleetSummarySchedule{Name: "Bad"}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid create error = %v", err)
	}
	if _, err := service.Update(t.Context(), "missing", 0, &schedule); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("missing update error = %v", err)
	}
	schedules := &fakeStore{schedules: []model.FleetSummarySchedule{{ID: "existing", Revision: 4, Name: "Old"}}}
	updating := newTestService(schedules, fakeNodes{}, fakeGroups{}, &fakeQueue{}, now)
	change := dailySchedule("UTC", "20:00")
	updated, err := updating.Update(t.Context(), "existing", 0, &change)
	if err != nil || updated.Revision != 5 {
		t.Fatalf("updated = %#v, error = %v", updated, err)
	}
}

func TestTestRunRequiresActorAndKeepsCursor(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	service := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	if _, err := service.TestRun(t.Context(), schedule.ID); !errors.Is(err, notificationqueue.ErrForbidden) {
		t.Fatalf("missing actor error = %v", err)
	}
	actor := store.EventMutationActor{UserID: "owner", SessionID: "session"}
	ctx := WithActor(t.Context(), actor)
	run, err := service.TestRun(ctx, schedule.ID)
	if err != nil || run.Kind != model.FleetSummaryRunTest || !strings.HasPrefix(run.PeriodKey, "test:") {
		t.Fatalf("test run = %#v, error = %v", run, err)
	}
	if len(schedules.tests) != 1 || schedules.tests[0].Actor != actor {
		t.Fatalf("recorded tests = %#v", schedules.tests)
	}
	if _, err := service.TestRun(ctx, schedule.ID); !errors.Is(err, notificationqueue.ErrRateLimited) {
		t.Fatalf("second test run error = %v", err)
	}
	if len(schedules.completions) != 0 {
		t.Fatalf("cursor advanced by test run: %#v", schedules.completions)
	}
}

func TestListHydratesDeliveryOutcome(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	schedule, schedules, queue := dueSummaryFixture(now)
	schedule.LastPeriodKey = "2026-07-29"
	schedules.schedules[0] = schedule
	queue.receipts["fleet_summary:schedule:2026-07-29"] = notificationqueue.Receipt{
		RoutingState: "routed", State: "succeeded", Counts: model.NotificationDeliveryCounts{Succeeded: 2}}
	service := newTestService(schedules, fakeNodes{}, fakeGroups{}, queue, now)
	listed, err := service.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].LastResult == nil || *listed[0].LastResult != model.FleetSummaryResultSuccess {
		t.Fatalf("listed = %#v, error = %v", listed, err)
	}
	if listed[0].DeliveryState == nil || *listed[0].DeliveryState != "succeeded" {
		t.Fatalf("delivery state = %#v", listed[0].DeliveryState)
	}
	queue.receipts["fleet_summary:schedule:2026-07-29"] = notificationqueue.Receipt{RoutingState: "not_routed"}
	queue.receiptErr = nil
	notRouted, err := service.List(t.Context())
	if err != nil || notRouted[0].LastResult == nil || *notRouted[0].LastResult != model.FleetSummaryResultFailed {
		t.Fatalf("not routed = %#v, error = %v", notRouted, err)
	}
	delete(queue.receipts, "fleet_summary:schedule:2026-07-29")
	unknown, err := service.List(t.Context())
	if err != nil || unknown[0].LastResult == nil || *unknown[0].LastResult != model.FleetSummaryResultUnknown {
		t.Fatalf("missing receipt = %#v, error = %v", unknown, err)
	}
}

func TestDeliveryOutcomeMapsOutboxStates(t *testing.T) {
	cases := []struct {
		receipt notificationqueue.Receipt
		result  string
	}{
		{notificationqueue.Receipt{Counts: model.NotificationDeliveryCounts{Pending: 1}}, model.FleetSummaryResultQueued},
		{notificationqueue.Receipt{Counts: model.NotificationDeliveryCounts{Leased: 1}}, model.FleetSummaryResultQueued},
		{notificationqueue.Receipt{Counts: model.NotificationDeliveryCounts{Succeeded: 1, Dead: 1}}, model.FleetSummaryResultPartial},
		{notificationqueue.Receipt{Counts: model.NotificationDeliveryCounts{Dead: 1}}, model.FleetSummaryResultFailed},
		{notificationqueue.Receipt{Counts: model.NotificationDeliveryCounts{}}, model.FleetSummaryResultUnknown},
	}
	for _, test := range cases {
		result, _ := deliveryOutcome(test.receipt)
		if result != test.result {
			t.Fatalf("result = %q, want %q", result, test.result)
		}
	}
}
