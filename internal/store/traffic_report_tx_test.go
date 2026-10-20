package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func reportCompletionFixture(t *testing.T) (*SQLiteStore, *TrafficReportScheduleStore, TrafficReportCompletion) {
	t.Helper()
	database := setupTestDB(t)
	schedules := NewTrafficReportScheduleStore(database.DB)
	now := model.NowUTC()
	schedule, err := schedules.Create(t.Context(), &model.TrafficReportSchedule{Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC",
		SendHour: 8, Weekday: 1, MonthDay: 1, AllNodes: true, AllChannels: true, Enabled: true, NextRunAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return database, schedules, TrafficReportCompletion{Schedule: *schedule, PeriodKey: "period", RunAt: now, NextRun: now.Add(24 * time.Hour)}
}

func TestReportCompletionSharesRollbackWithOutbox(t *testing.T) {
	database, schedules, completion := reportCompletionFixture(t)
	transaction := alertTestTx(t, database.DB)
	completed, err := schedules.CompletePeriodTx(t.Context(), transaction, completion)
	if err != nil || !completed {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	stored, err := schedules.Get(t.Context(), completion.Schedule.ID)
	if err != nil || stored.LastPeriodKey != "" || !stored.NextRunAt.Equal(completion.Schedule.NextRunAt) {
		t.Fatal("failed outbox would lose the report period")
	}
}

func TestReportCompletionRejectsStaleScheduleAndDuplicatePeriod(t *testing.T) {
	database, schedules, completion := reportCompletionFixture(t)
	transaction := alertTestTx(t, database.DB)
	completed, err := schedules.CompletePeriodTx(t.Context(), transaction, completion)
	if err != nil || !completed {
		t.Fatal(err)
	}
	if again, err := schedules.CompletePeriodTx(t.Context(), transaction, completion); err != nil || again {
		t.Fatal("duplicate report period advanced")
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	stored, err := schedules.Get(t.Context(), completion.Schedule.ID)
	if err != nil || stored.LastDelivery == nil || stored.LastDelivery.State != "queued" {
		t.Fatal("queued report claims delivery success")
	}
}

func TestReportCompletionCannotOverwriteEditedTargets(t *testing.T) {
	database, schedules, completion := reportCompletionFixture(t)
	updated := completion.Schedule
	updated.Name = "New configuration"
	if _, err := schedules.Update(t.Context(), updated.ID, &updated); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, database.DB)
	if completed, err := schedules.CompletePeriodTx(t.Context(), transaction, completion); err != nil || completed {
		t.Fatal("stale snapshot advanced edited schedule")
	}
}

func TestReportIgnoredWriteReturnsFailure(t *testing.T) {
	database, schedules, completion := reportCompletionFixture(t)
	if _, err := database.DB.Exec(`CREATE TRIGGER ignore_report_update BEFORE UPDATE ON traffic_report_schedules BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, database.DB)
	if completed, err := schedules.CompletePeriodTx(t.Context(), transaction, completion); err == nil || completed {
		t.Fatal("ignored report update looked successful")
	}
}
