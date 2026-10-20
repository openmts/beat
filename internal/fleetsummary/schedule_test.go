package fleetsummary

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func dailySchedule(timezone, localTime string) model.FleetSummarySchedule {
	return model.FleetSummarySchedule{
		Name: "Daily", Cadence: model.FleetSummaryDaily, Timezone: timezone, LocalTime: localTime,
		NameThreshold: model.FleetSummaryDefaultNameThreshold, Enabled: true, ChannelIDs: []string{"channel"},
	}
}

func weeklySchedule(weekday int, localTime string) model.FleetSummarySchedule {
	schedule := dailySchedule("UTC", localTime)
	schedule.Cadence = model.FleetSummaryWeekly
	schedule.Weekday = &weekday
	return schedule
}

func TestValidateScheduleRejectsInvalidConfigurations(t *testing.T) {
	valid := dailySchedule("UTC", "08:00")
	if err := ValidateSchedule(valid); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	weekday := 3
	cases := map[string]model.FleetSummarySchedule{
		"empty name":         func() model.FleetSummarySchedule { s := valid; s.Name = "  "; return s }(),
		"bad local time":     func() model.FleetSummarySchedule { s := valid; s.LocalTime = "25:00"; return s }(),
		"padded minute":      func() model.FleetSummarySchedule { s := valid; s.LocalTime = "8:00"; return s }(),
		"unknown timezone":   func() model.FleetSummarySchedule { s := valid; s.Timezone = "not-a-zone"; return s }(),
		"unknown cadence":    func() model.FleetSummarySchedule { s := valid; s.Cadence = "hourly"; return s }(),
		"daily weekday":      func() model.FleetSummarySchedule { s := valid; s.Weekday = &weekday; return s }(),
		"weekly no weekday":  func() model.FleetSummarySchedule { s := valid; s.Cadence = model.FleetSummaryWeekly; return s }(),
		"weekly weekday out": func() model.FleetSummarySchedule { s := weeklySchedule(7, "08:00"); return s }(),
		"threshold too high": func() model.FleetSummarySchedule {
			s := valid
			s.NameThreshold = model.FleetSummaryMaximumNameThreshold + 1
			return s
		}(),
		"no channels": func() model.FleetSummarySchedule { s := valid; s.ChannelIDs = nil; return s }(),
	}
	for name, schedule := range cases {
		if err := ValidateSchedule(schedule); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestNextRunDailyAndWeeklyAcrossCalendarBoundaries(t *testing.T) {
	schedule := dailySchedule("UTC", "08:00")
	next, err := NextRun(schedule, time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily next = %v, error = %v", next, err)
	}
	next, err = NextRun(schedule, time.Date(2026, 12, 31, 8, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2027, 1, 1, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("year boundary next = %v, error = %v", next, err)
	}
	next, err = NextRun(dailySchedule("Asia/Shanghai", "08:00"), time.Date(2026, 7, 30, 0, 30, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("zoned next = %v, error = %v", next, err)
	}

	wednesday := weeklySchedule(3, "08:00")
	next, err = NextRun(wednesday, time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekly next = %v, error = %v", next, err)
	}
	next, err = NextRun(wednesday, time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekly rollover = %v, error = %v", next, err)
	}
}

func TestNextRunHandlesDaylightSavingGapsAndRepeats(t *testing.T) {
	// 2026-03-08 02:30 America/New_York 不存在：推进到当日第一个有效瞬间 03:00 EDT。
	gap := dailySchedule("America/New_York", "02:30")
	next, err := NextRun(gap, time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("gap next = %v, error = %v", next, err)
	}
	// 2026-11-01 01:30 America/New_York 出现两次：取第一次（EDT）。
	repeat := dailySchedule("America/New_York", "01:30")
	next, err = NextRun(repeat, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)) {
		t.Fatalf("repeat next = %v, error = %v", next, err)
	}
	// 第一次出现之后当天不再重复触发，直接推进到次日。
	next, err = NextRun(repeat, time.Date(2026, 11, 1, 5, 45, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("post repeat next = %v, error = %v", next, err)
	}
}

func TestPeriodKeyUsesLocalCalendarAndISOWeeks(t *testing.T) {
	run := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	key, err := PeriodKey(dailySchedule("UTC", "08:00"), run)
	if err != nil || key != "2026-07-30" {
		t.Fatalf("daily key = %q, error = %v", key, err)
	}
	key, err = PeriodKey(dailySchedule("Asia/Shanghai", "08:00"), run)
	if err != nil || key != "2026-07-30" {
		t.Fatalf("zoned key = %q, error = %v", key, err)
	}
	key, err = PeriodKey(dailySchedule("Pacific/Kiritimati", "08:00"), run)
	if err != nil || key != "2026-07-30" {
		t.Fatalf("far east key = %q, error = %v", key, err)
	}
	key, err = PeriodKey(weeklySchedule(4, "08:00"), run)
	if err != nil || key != "2026-W31" {
		t.Fatalf("weekly key = %q, error = %v", key, err)
	}
	key, err = PeriodKey(weeklySchedule(5, "08:00"), time.Date(2027, 1, 1, 8, 0, 0, 0, time.UTC))
	if err != nil || key != "2026-W53" {
		t.Fatalf("cross year key = %q, error = %v", key, err)
	}
}

func TestNameListHonoursBoundedThreshold(t *testing.T) {
	names := []string{"alpha", "beta"}
	if got := nameList(nil, 10); got != "none" {
		t.Fatalf("empty list = %q", got)
	}
	if got := nameList(names, 0); got != "2 nodes (names omitted)" {
		t.Fatalf("zero threshold = %q", got)
	}
	if got := nameList(names, 2); got != "alpha, beta" {
		t.Fatalf("exact threshold = %q", got)
	}
	if got := nameList(names, 1); got != "2 nodes (names omitted)" {
		t.Fatalf("low threshold = %q", got)
	}
}
