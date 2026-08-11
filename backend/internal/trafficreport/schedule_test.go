package trafficreport

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestNextRunUsesConfiguredLocalTime(t *testing.T) {
	schedule := model.TrafficReportSchedule{
		Cadence: model.TrafficReportDaily, Timezone: "Asia/Shanghai",
		SendHour: 8, SendMinute: 30,
	}
	next, err := NextRun(schedule, time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next run: %v", err)
	}
	want := time.Date(2026, 7, 30, 0, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %v, want %v", next, want)
	}
}

func TestNextRunWeeklyAndMonthlyClamp(t *testing.T) {
	weekly := model.TrafficReportSchedule{
		Cadence: model.TrafficReportWeekly, Timezone: "UTC",
		SendHour: 9, Weekday: 1,
	}
	next, err := NextRun(weekly, time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("weekly next run: %v", err)
	}
	if want := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("weekly next run = %v, want %v", next, want)
	}

	monthly := model.TrafficReportSchedule{
		Cadence: model.TrafficReportMonthly, Timezone: "UTC",
		SendHour: 8, MonthDay: 31,
	}
	next, err = NextRun(monthly, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("monthly next run: %v", err)
	}
	if want := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("monthly next run = %v, want %v", next, want)
	}
}

func TestReportPeriodUsesCompletedLocalPeriod(t *testing.T) {
	tests := []struct {
		name     string
		schedule model.TrafficReportSchedule
		runAt    time.Time
		start    time.Time
		end      time.Time
	}{
		{
			name: "daily", schedule: model.TrafficReportSchedule{
				Cadence: model.TrafficReportDaily, Timezone: "Asia/Shanghai",
			}, runAt: time.Date(2026, 7, 30, 0, 30, 0, 0, time.UTC),
			start: time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 7, 29, 16, 0, 0, 0, time.UTC),
		},
		{
			name: "weekly", schedule: model.TrafficReportSchedule{
				Cadence: model.TrafficReportWeekly, Timezone: "UTC",
			}, runAt: time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC),
			start: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "monthly", schedule: model.TrafficReportSchedule{
				Cadence: model.TrafficReportMonthly, Timezone: "UTC",
			}, runAt: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
			start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			period, err := ReportPeriod(test.schedule, test.runAt)
			if err != nil {
				t.Fatalf("report period: %v", err)
			}
			if !period.Start.Equal(test.start) || !period.End.Equal(test.end) {
				t.Fatalf("period = %v..%v, want %v..%v", period.Start, period.End, test.start, test.end)
			}
		})
	}
}

func TestValidateScheduleRejectsInvalidScopeAndTimezone(t *testing.T) {
	schedule := model.TrafficReportSchedule{
		Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "Missing/Zone",
		SendHour: 8, AllNodes: false, AllChannels: false,
	}
	if err := ValidateSchedule(schedule); err == nil {
		t.Fatal("expected invalid schedule error")
	}
	schedule.Timezone = "UTC"
	schedule.NodeIDs = []string{"node-1"}
	schedule.ChannelIDs = []string{"channel-1"}
	if err := ValidateSchedule(schedule); err != nil {
		t.Fatalf("valid schedule: %v", err)
	}
}

func TestPreviousRunReturnsLatestCompletedExecution(t *testing.T) {
	schedule := model.TrafficReportSchedule{
		Cadence: model.TrafficReportDaily, Timezone: "UTC", SendHour: 8,
	}
	previous, err := PreviousRun(schedule, time.Date(2026, 7, 30, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("previous run: %v", err)
	}
	want := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	if !previous.Equal(want) {
		t.Fatalf("previous run = %v, want %v", previous, want)
	}
}

func TestScheduleCadenceValidationAndPreviousRuns(t *testing.T) {
	base := model.TrafficReportSchedule{
		Name: "Report", Timezone: "UTC", SendHour: 8,
		AllNodes: true, AllChannels: true,
	}
	tests := []struct {
		name     string
		schedule model.TrafficReportSchedule
	}{
		{name: "invalid time", schedule: mergeSchedule(base, "daily", -1, 1, 1)},
		{name: "invalid cadence", schedule: mergeSchedule(base, "yearly", 8, 1, 1)},
		{name: "invalid weekly day", schedule: mergeSchedule(base, "weekly", 8, 0, 1)},
		{name: "invalid monthly day", schedule: mergeSchedule(base, "monthly", 8, 1, 32)},
		{name: "missing nodes", schedule: func() model.TrafficReportSchedule {
			value := mergeSchedule(base, "daily", 8, 1, 1)
			value.AllNodes = false
			return value
		}()},
		{name: "missing channels", schedule: func() model.TrafficReportSchedule {
			value := mergeSchedule(base, "daily", 8, 1, 1)
			value.AllChannels = false
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateSchedule(test.schedule); err == nil {
				t.Fatal("ValidateSchedule() error = nil")
			}
		})
	}

	for _, schedule := range []model.TrafficReportSchedule{
		mergeSchedule(base, model.TrafficReportWeekly, 8, 5, 1),
		mergeSchedule(base, model.TrafficReportMonthly, 8, 1, 31),
	} {
		previous, err := PreviousRun(schedule, time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))
		if err != nil || previous.IsZero() {
			t.Fatalf("previous run = %v, error = %v", previous, err)
		}
	}
	invalid := mergeSchedule(base, "bad", 8, 1, 1)
	if _, err := PreviousRun(invalid, time.Now()); err == nil {
		t.Fatal("PreviousRun() cadence error = nil")
	}
	if _, err := ReportPeriod(invalid, time.Now()); err == nil {
		t.Fatal("ReportPeriod() cadence error = nil")
	}
}

func mergeSchedule(
	base model.TrafficReportSchedule,
	cadence string,
	hour int,
	weekday int,
	monthDay int,
) model.TrafficReportSchedule {
	base.Cadence = cadence
	base.SendHour = hour
	base.Weekday = weekday
	base.MonthDay = monthDay
	return base
}
