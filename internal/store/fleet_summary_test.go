package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func fleetSummaryFixture(t *testing.T) (*SQLiteStore, *FleetSummaryStore, model.FleetSummarySchedule, time.Time) {
	t.Helper()
	database := setupTestDB(t)
	node, err := NewNodeStore(database.DB).UpsertNode(t.Context(), "summary-node", "192.0.2.10", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	group, err := NewGroupStore(database.DB).GetDefaultGroup(t.Context())
	if err != nil || group == nil {
		t.Fatalf("default group = %#v, error = %v", group, err)
	}
	channel, err := NewAlertChannelStore(database.DB).CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Summary", ChannelType: "webhook", Config: `{"url":"https://example.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	nextRun := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	schedule := model.FleetSummarySchedule{
		Name: "Morning", Cadence: model.FleetSummaryDaily, Timezone: "UTC", LocalTime: "08:00",
		NameThreshold: model.FleetSummaryDefaultNameThreshold, Enabled: true,
		NodeIDs: []string{node.ID}, GroupIDs: []string{group.ID}, ChannelIDs: []string{channel.ID},
		NextRunAt: &nextRun,
	}
	return database, NewFleetSummaryStore(database.DB), schedule, nextRun
}

func TestFleetSummaryScheduleLifecycleAndRevision(t *testing.T) {
	database, schedules, schedule, nextRun := fleetSummaryFixture(t)
	created, err := schedules.Create(t.Context(), &schedule)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if created.Revision != 1 || created.ID == "" || created.LastResult != nil {
		t.Fatalf("created schedule = %#v", created)
	}
	listed, err := schedules.List(t.Context())
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed schedules = %#v, error = %v", listed, err)
	}
	if len(listed[0].NodeIDs) != 1 || len(listed[0].GroupIDs) != 1 || len(listed[0].ChannelIDs) != 1 {
		t.Fatalf("listed scopes = %#v", listed[0])
	}
	due, err := schedules.ListDue(t.Context(), nextRun)
	if err != nil || len(due) != 1 {
		t.Fatalf("due schedules = %#v, error = %v", due, err)
	}
	if missed, err := schedules.ListDue(t.Context(), nextRun.Add(-time.Hour)); err != nil || len(missed) != 0 {
		t.Fatalf("early due schedules = %#v, error = %v", missed, err)
	}

	change := *created
	change.Name = "Evening"
	change.LocalTime = "20:00"
	change.NodeIDs = []string{}
	change.NameThreshold = 0
	updated, err := schedules.Update(t.Context(), created.ID, created.Revision, &change)
	if err != nil || updated == nil || updated.Revision != 2 || updated.Name != "Evening" || len(updated.NodeIDs) != 0 {
		t.Fatalf("updated schedule = %#v, error = %v", updated, err)
	}
	if _, err := schedules.Update(t.Context(), created.ID, created.Revision, &change); !errors.Is(err, ErrFleetSummaryConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
	if err := schedules.Delete(t.Context(), created.ID, created.Revision); !errors.Is(err, ErrFleetSummaryConflict) {
		t.Fatalf("stale delete error = %v, want conflict", err)
	}
	if err := schedules.Delete(t.Context(), "missing-id", 1); !errors.Is(err, ErrFleetSummaryNotFound) {
		t.Fatalf("missing delete error = %v, want not found", err)
	}
	if err := schedules.Delete(t.Context(), created.ID, updated.Revision); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}
	if remaining, err := schedules.List(t.Context()); err != nil || len(remaining) != 0 {
		t.Fatalf("remaining schedules = %#v, error = %v", remaining, err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM fleet_summary_channels").Scan(new(int)); err != nil {
		t.Fatalf("query scopes: %v", err)
	}
}

func TestFleetSummaryCompletionClaimsOnceAndAdvancesCursor(t *testing.T) {
	database, schedules, schedule, nextRun := fleetSummaryFixture(t)
	created, err := schedules.Create(t.Context(), &schedule)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	loaded, err := schedules.Get(t.Context(), created.ID)
	if err != nil || loaded == nil {
		t.Fatalf("get schedule = %#v, error = %v", loaded, err)
	}
	runAt := nextRun.Add(time.Second)
	completion := FleetSummaryCompletion{
		Schedule: *loaded, PeriodKey: "2026-07-30", NextRun: nextRun.Add(24 * time.Hour), RunAt: runAt,
		Run: model.FleetSummaryRun{ScheduleID: loaded.ID, ScheduleName: loaded.Name, Cadence: loaded.Cadence,
			Timezone: loaded.Timezone, Kind: model.FleetSummaryRunScheduled,
			Counts:       model.FleetSummaryCounts{Online: 1, Total: 1, OnlineNames: []string{"summary-node"}, OfflineNames: []string{}},
			ChannelCount: 1},
	}
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	claimed, err := schedules.CompleteRunTx(t.Context(), tx, completion)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, error = %v", claimed, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	after, err := schedules.Get(t.Context(), created.ID)
	if err != nil || after == nil || after.LastPeriodKey != "2026-07-30" || after.LastResult == nil {
		t.Fatalf("schedule after claim = %#v, error = %v", after, err)
	}
	if after.NextRunAt == nil || !after.NextRunAt.Equal(nextRun.Add(24*time.Hour)) {
		t.Fatalf("next run = %v", after.NextRunAt)
	}
	duplicate, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin duplicate: %v", err)
	}
	replayed, err := schedules.CompleteRunTx(t.Context(), duplicate, completion)
	if err != nil || replayed {
		t.Fatalf("second claim = %v, error = %v", replayed, err)
	}
	if err := duplicate.Rollback(); err != nil {
		t.Fatalf("rollback duplicate: %v", err)
	}
	var runs int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM fleet_summary_runs").Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("run count = %d, error = %v", runs, err)
	}
	if _, err := schedules.CompleteRunTx(t.Context(), nil, completion); err == nil {
		t.Fatal("nil transaction must be rejected")
	}
}

func TestFleetSummaryTestRunAndRestoreReconciliation(t *testing.T) {
	database, schedules, schedule, nextRun := fleetSummaryFixture(t)
	created, err := schedules.Create(t.Context(), &schedule)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	owner := catalogPolicyWrite(t, NewMetricCatalogStore(database.DB), model.MetricPolicyUpdate{})
	actor := EventMutationActor{UserID: owner.Audit.ActorID, SessionID: owner.SessionID}
	at := nextRun.Add(-time.Hour)
	test := FleetSummaryTest{ScheduleID: created.ID, Actor: actor, At: at,
		Run: model.FleetSummaryRun{ScheduleID: created.ID, ScheduleName: created.Name, Cadence: created.Cadence,
			Timezone: created.Timezone, PeriodKey: "test:fixture", Cutoff: at,
			Kind: model.FleetSummaryRunTest, Counts: model.FleetSummaryCounts{Total: 0,
				OnlineNames: []string{}, OfflineNames: []string{}}}}
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := schedules.RecordTestRunTx(t.Context(), tx, test); err != nil {
		t.Fatalf("record test run: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	after, err := schedules.Get(t.Context(), created.ID)
	if err != nil || after == nil || after.LastPeriodKey != "" {
		t.Fatalf("cursor advanced by test run: %#v, error = %v", after, err)
	}
	if err := ReconcileFleetSummaryRestore(t.Context(), database.path, at.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("reconcile restore: %v", err)
	}
	var status, code string
	if err := database.DB.QueryRow(
		"SELECT status, result_code FROM fleet_summary_runs LIMIT 1").Scan(&status, &code); err != nil {
		t.Fatalf("query run: %v", err)
	}
	if status != model.FleetSummaryResultFailed || code != "delivery_unknown" {
		t.Fatalf("settled run = %q/%q", status, code)
	}
}
