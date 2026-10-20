package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func installFleetFault(t *testing.T, database *SQLiteStore, statement string) {
	t.Helper()
	if _, err := database.DB.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func installFleetCommitFault(t *testing.T, database *SQLiteStore, target string) {
	t.Helper()
	installFleetFault(t, database, `CREATE TABLE cov_fleet_child(
		schedule TEXT REFERENCES fleet_summary_schedules(id) DEFERRABLE INITIALLY DEFERRED)`)
	installFleetFault(t, database, `CREATE TRIGGER cov_fleet_commit AFTER `+target+
		` BEGIN INSERT INTO cov_fleet_child VALUES('missing-schedule'); END`)
}

// TestFleetSummaryCreateFaults 覆盖计划创建的事务、写入、作用域与提交失败。
func TestFleetSummaryCreateFaults(t *testing.T) {
	t.Run("missing next run", func(t *testing.T) {
		_, schedules, schedule, _ := fleetSummaryFixture(t)
		schedule.NextRunAt = nil
		if _, err := schedules.Create(t.Context(), &schedule); err == nil {
			t.Fatal("schedule without next run was created")
		}
	})
	t.Run("closed database", func(t *testing.T) {
		_, _, schedule, _ := fleetSummaryFixture(t)
		if _, err := NewFleetSummaryStore(closedSQLiteStore(t).DB).Create(t.Context(), &schedule); err == nil {
			t.Fatal("closed database created a schedule")
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_insert
			BEFORE INSERT ON fleet_summary_schedules BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		if _, err := schedules.Create(t.Context(), &schedule); err == nil {
			t.Fatal("blocked schedule insert was accepted")
		}
	})
	t.Run("scope insert blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_scope_insert
			BEFORE INSERT ON fleet_summary_nodes BEGIN SELECT RAISE(ABORT,'scope blocked'); END`)
		if _, err := schedules.Create(t.Context(), &schedule); err == nil {
			t.Fatal("blocked scope insert was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		installFleetCommitFault(t, database, "INSERT ON fleet_summary_schedules")
		if _, err := schedules.Create(t.Context(), &schedule); err == nil {
			t.Fatal("deferred schedule commit failure was accepted")
		}
	})
	t.Run("weekday retained", func(t *testing.T) {
		_, schedules, schedule, _ := fleetSummaryFixture(t)
		weekday := 3
		schedule.Cadence = model.FleetSummaryWeekly
		schedule.Weekday = &weekday
		created, err := schedules.Create(t.Context(), &schedule)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := schedules.Get(t.Context(), created.ID)
		if err != nil || loaded == nil || loaded.Weekday == nil || *loaded.Weekday != weekday {
			t.Fatalf("weekday = %#v, error = %v", loaded, err)
		}
	})
}

// TestFleetSummaryUpdateFaults 覆盖计划更新的事务、写入、作用域与提交失败。
func TestFleetSummaryUpdateFaults(t *testing.T) {
	load := func(t *testing.T, database *SQLiteStore, schedules *FleetSummaryStore, schedule model.FleetSummarySchedule) model.FleetSummarySchedule {
		t.Helper()
		created := covFleetSchedule(t, database, schedules, schedule)
		loaded, err := schedules.Get(t.Context(), created.ID)
		if err != nil || loaded == nil {
			t.Fatalf("load schedule = %#v, error = %v", loaded, err)
		}
		return *loaded
	}
	t.Run("missing next run", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		loaded := load(t, database, schedules, schedule)
		loaded.NextRunAt = nil
		if _, err := schedules.Update(t.Context(), loaded.ID, loaded.Revision, &loaded); err == nil {
			t.Fatal("schedule without next run was updated")
		}
	})
	t.Run("closed database", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		loaded := load(t, database, schedules, schedule)
		if _, err := NewFleetSummaryStore(closedSQLiteStore(t).DB).Update(t.Context(),
			loaded.ID, loaded.Revision, &loaded); err == nil {
			t.Fatal("closed database updated a schedule")
		}
	})
	t.Run("update blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		loaded := load(t, database, schedules, schedule)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_update
			BEFORE UPDATE ON fleet_summary_schedules BEGIN SELECT RAISE(ABORT,'update blocked'); END`)
		if _, err := schedules.Update(t.Context(), loaded.ID, loaded.Revision, &loaded); err == nil {
			t.Fatal("blocked schedule update was accepted")
		}
	})
	t.Run("scope clear blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		loaded := load(t, database, schedules, schedule)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_scope_clear
			BEFORE DELETE ON fleet_summary_nodes BEGIN SELECT RAISE(ABORT,'scope blocked'); END`)
		if _, err := schedules.Update(t.Context(), loaded.ID, loaded.Revision, &loaded); err == nil {
			t.Fatal("blocked scope clear was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		loaded := load(t, database, schedules, schedule)
		installFleetCommitFault(t, database, "UPDATE ON fleet_summary_schedules")
		if _, err := schedules.Update(t.Context(), loaded.ID, loaded.Revision, &loaded); err == nil {
			t.Fatal("deferred schedule update commit failure was accepted")
		}
	})
}

// TestFleetSummaryDeleteFaults 覆盖计划删除的事务、写入、作用域与提交失败。
func TestFleetSummaryDeleteFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		if err := NewFleetSummaryStore(closedSQLiteStore(t).DB).Delete(t.Context(), "missing", 1); err == nil {
			t.Fatal("closed database deleted a schedule")
		}
	})
	t.Run("delete blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		created := covFleetSchedule(t, database, schedules, schedule)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_delete
			BEFORE DELETE ON fleet_summary_schedules BEGIN SELECT RAISE(ABORT,'delete blocked'); END`)
		if err := schedules.Delete(t.Context(), created.ID, created.Revision); err == nil {
			t.Fatal("blocked schedule delete was accepted")
		}
	})
	t.Run("scope delete blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		created := covFleetSchedule(t, database, schedules, schedule)
		installFleetFault(t, database, `CREATE TRIGGER cov_fleet_scope_delete
			BEFORE DELETE ON fleet_summary_nodes BEGIN SELECT RAISE(ABORT,'scope blocked'); END`)
		if err := schedules.Delete(t.Context(), created.ID, created.Revision); err == nil {
			t.Fatal("blocked scope delete was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		database, schedules, schedule, _ := fleetSummaryFixture(t)
		created := covFleetSchedule(t, database, schedules, schedule)
		installFleetCommitFault(t, database, "DELETE ON fleet_summary_schedules")
		if err := schedules.Delete(t.Context(), created.ID, created.Revision); err == nil {
			t.Fatal("deferred schedule delete commit failure was accepted")
		}
	})
}

// TestFleetSummaryConflictQueryFaults 覆盖版本冲突时存在性查询的失败。
func TestFleetSummaryConflictQueryFaults(t *testing.T) {
	database, schedules, _, _ := fleetSummaryFixture(t)
	installFleetFault(t, database, "ALTER TABLE fleet_summary_schedules RENAME TO cov_fleet_backup")
	if err := schedules.writeConflict(t.Context(), "missing"); err == nil || errors.Is(err, ErrFleetSummaryNotFound) {
		t.Fatalf("conflict query error = %v", err)
	}
}
