package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const covFleetPeriodKey = "2026-07-30"

func covFleetClaim(schedule model.FleetSummarySchedule, nextRun time.Time) FleetSummaryCompletion {
	return FleetSummaryCompletion{
		Schedule: schedule, PeriodKey: covFleetPeriodKey, NextRun: nextRun, RunAt: schedule.NextRunAt.Add(time.Second),
		Run: model.FleetSummaryRun{ScheduleName: schedule.Name, Cadence: schedule.Cadence, Timezone: schedule.Timezone,
			Kind: model.FleetSummaryRunScheduled},
	}
}

func covFleetSchedule(t *testing.T, database *SQLiteStore, schedules *FleetSummaryStore, schedule model.FleetSummarySchedule) model.FleetSummarySchedule {
	t.Helper()
	created, err := schedules.Create(t.Context(), &schedule)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := schedules.Get(t.Context(), created.ID)
	if err != nil || loaded == nil {
		t.Fatalf("load schedule = %#v, error = %v", loaded, err)
	}
	return *loaded
}

func TestFleetSummaryCompletionRejectsInvalidClaims(t *testing.T) {
	database, schedules, schedule, nextRun := fleetSummaryFixture(t)
	loaded := covFleetSchedule(t, database, schedules, schedule)
	valid := covFleetClaim(loaded, nextRun.Add(24*time.Hour))
	cases := []struct {
		name   string
		mutate func(*FleetSummaryCompletion)
	}{
		{"missing period key", func(c *FleetSummaryCompletion) { c.PeriodKey = "" }},
		{"missing schedule identity", func(c *FleetSummaryCompletion) { c.Schedule.ID = "" }},
		{"missing schedule deadline", func(c *FleetSummaryCompletion) { c.Schedule.NextRunAt = nil }},
		{"missing run time", func(c *FleetSummaryCompletion) { c.RunAt = time.Time{} }},
		{"cursor not advanced", func(c *FleetSummaryCompletion) { c.NextRun = nextRun }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			completion := valid
			test.mutate(&completion)
			transaction := alertTestTx(t, database.DB)
			claimed, err := schedules.CompleteRunTx(t.Context(), transaction, completion)
			if err == nil || claimed {
				t.Fatalf("invalid completion claimed = %v, error = %v", claimed, err)
			}
		})
	}
	if claimed, err := schedules.CompleteRunTx(t.Context(), nil, valid); err == nil || claimed {
		t.Fatalf("nil transaction claimed = %v, error = %v", claimed, err)
	}
}

func TestFleetSummaryCompletionSurfacesStorageFailures(t *testing.T) {
	database, schedules, schedule, nextRun := fleetSummaryFixture(t)
	completion := covFleetClaim(covFleetSchedule(t, database, schedules, schedule), nextRun.Add(24*time.Hour))
	transaction := alertTestTx(t, database.DB)
	if _, err := transaction.ExecContext(t.Context(), `CREATE TRIGGER cov_block_fleet_claim BEFORE UPDATE ON fleet_summary_schedules BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if claimed, err := schedules.CompleteRunTx(t.Context(), transaction, completion); err == nil || claimed {
		t.Fatalf("blocked claim = %v, error = %v", claimed, err)
	}
	if _, err := transaction.ExecContext(t.Context(), "DROP TRIGGER cov_block_fleet_claim"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), `CREATE TRIGGER cov_block_fleet_run BEFORE INSERT ON fleet_summary_runs BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if claimed, err := schedules.CompleteRunTx(t.Context(), transaction, completion); err == nil || claimed {
		t.Fatalf("blocked run audit = %v, error = %v", claimed, err)
	}
}

func TestFleetSummaryTestRunRequiresScheduleAndActor(t *testing.T) {
	database, schedules, schedule, _ := fleetSummaryFixture(t)
	loaded := covFleetSchedule(t, database, schedules, schedule)
	owner := catalogPolicyWrite(t, NewMetricCatalogStore(database.DB), model.MetricPolicyUpdate{})
	transaction := alertTestTx(t, database.DB)
	if err := schedules.RecordTestRunTx(t.Context(), nil, FleetSummaryTest{ScheduleID: loaded.ID, At: model.NowUTC()}); err == nil {
		t.Fatal("nil transaction accepted a test run")
	}
	unknown := FleetSummaryTest{ScheduleID: loaded.ID, Actor: EventMutationActor{UserID: "missing-user", SessionID: "missing-session"}, At: model.NowUTC()}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, unknown); err == nil {
		t.Fatal("unknown actor accepted a test run")
	}
	absent := FleetSummaryTest{ScheduleID: "missing-schedule", Actor: EventMutationActor{UserID: owner.Audit.ActorID, SessionID: owner.SessionID}, At: model.NowUTC()}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, absent); err == nil {
		t.Fatal("missing schedule accepted a test run")
	}
	valid := FleetSummaryTest{ScheduleID: loaded.ID, Actor: EventMutationActor{UserID: owner.Audit.ActorID, SessionID: owner.SessionID}, At: model.NowUTC(),
		Run: model.FleetSummaryRun{ScheduleName: loaded.Name, Cadence: loaded.Cadence, Timezone: loaded.Timezone, PeriodKey: covFleetPeriodKey}}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, valid); err != nil {
		t.Fatalf("test run: %v", err)
	}
	var kind string
	if err := transaction.QueryRowContext(t.Context(), "SELECT kind FROM fleet_summary_runs WHERE schedule_id=?", loaded.ID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != model.FleetSummaryRunTest {
		t.Fatalf("run kind = %q", kind)
	}
	var audits int
	if err := transaction.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM admin_audit_events WHERE action='fleet_summary.test_queued'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("test run audits = %d", audits)
	}
	if _, err := transaction.ExecContext(t.Context(), `CREATE TRIGGER cov_block_fleet_test_run BEFORE INSERT ON fleet_summary_runs BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := schedules.RecordTestRunTx(t.Context(), transaction, valid); err == nil {
		t.Fatal("blocked test run insert succeeded")
	}
}

func TestReconcileFleetSummaryRestoreHandlesMissingTableAndBrokenTrim(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(directory, "empty.db")
	if handle, err := sql.Open("sqlite", empty); err != nil {
		t.Fatal(err)
	} else {
		if _, err := handle.Exec("CREATE TABLE unrelated(id TEXT)"); err != nil {
			t.Fatal(err)
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileFleetSummaryRestore(t.Context(), empty, model.NowUTC()); err != nil {
		t.Fatalf("restore without fleet summary runs: %v", err)
	}
	blocked := filepath.Join(directory, "blocked.db")
	handle, err := sql.Open("sqlite", blocked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`CREATE TABLE fleet_summary_runs(id TEXT PRIMARY KEY, status TEXT NOT NULL, result_code TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL, completed_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO fleet_summary_runs(id,status,result_code,created_at,completed_at) VALUES('ancient','queued','',0,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`CREATE TRIGGER cov_block_fleet_trim BEFORE DELETE ON fleet_summary_runs BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileFleetSummaryRestore(t.Context(), blocked, model.NowUTC()); err == nil {
		t.Fatal("blocked retention trim succeeded")
	}
	if err := ReconcileFleetSummaryRestore(t.Context(), directory, model.NowUTC()); err == nil {
		t.Fatal("reconcile of a directory succeeded")
	}
}
