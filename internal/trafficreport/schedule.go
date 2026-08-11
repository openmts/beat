package trafficreport

import (
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

func ValidateSchedule(schedule model.TrafficReportSchedule) error {
	if schedule.Name == "" {
		return fmt.Errorf("schedule name is required")
	}
	if schedule.SendHour < 0 || schedule.SendHour > 23 ||
		schedule.SendMinute < 0 || schedule.SendMinute > 59 {
		return fmt.Errorf("send time is invalid")
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return fmt.Errorf("load schedule timezone: %w", err)
	}
	if err := validateCadence(schedule); err != nil {
		return err
	}
	if !schedule.AllNodes && len(schedule.NodeIDs) == 0 {
		return fmt.Errorf("at least one node is required")
	}
	if !schedule.AllChannels && len(schedule.ChannelIDs) == 0 {
		return fmt.Errorf("at least one channel is required")
	}
	return nil
}

func validateCadence(schedule model.TrafficReportSchedule) error {
	switch schedule.Cadence {
	case model.TrafficReportDaily:
		return nil
	case model.TrafficReportWeekly:
		if schedule.Weekday < 1 || schedule.Weekday > 7 {
			return fmt.Errorf("weekday must be between 1 and 7")
		}
	case model.TrafficReportMonthly:
		if schedule.MonthDay < 1 || schedule.MonthDay > 31 {
			return fmt.Errorf("month day must be between 1 and 31")
		}
	default:
		return fmt.Errorf("invalid report cadence")
	}
	return nil
}

func NextRun(schedule model.TrafficReportSchedule, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load schedule timezone: %w", err)
	}
	localAfter := after.In(location)
	var candidate time.Time
	switch schedule.Cadence {
	case model.TrafficReportDaily:
		candidate = localClock(localAfter, schedule, 0)
		if !candidate.After(localAfter) {
			candidate = candidate.AddDate(0, 0, 1)
		}
	case model.TrafficReportWeekly:
		candidate = weeklyCandidate(localAfter, schedule)
	case model.TrafficReportMonthly:
		candidate = monthlyCandidate(localAfter, schedule)
	default:
		return time.Time{}, fmt.Errorf("invalid report cadence")
	}
	return candidate.UTC(), nil
}

func PreviousRun(schedule model.TrafficReportSchedule, at time.Time) (time.Time, error) {
	next, err := NextRun(schedule, at)
	if err != nil {
		return time.Time{}, err
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load schedule timezone: %w", err)
	}
	localNext := next.In(location)
	var previous time.Time
	switch schedule.Cadence {
	case model.TrafficReportDaily:
		previous = localNext.AddDate(0, 0, -1)
	case model.TrafficReportWeekly:
		previous = localNext.AddDate(0, 0, -7)
	case model.TrafficReportMonthly:
		base := time.Date(localNext.Year(), localNext.Month(), 1, 0, 0, 0, 0, location).AddDate(0, -1, 0)
		previous = localMonthClock(base.Year(), base.Month(), location, schedule)
	default:
		return time.Time{}, fmt.Errorf("invalid report cadence")
	}
	return previous.UTC(), nil
}

func ReportPeriod(
	schedule model.TrafficReportSchedule,
	runAt time.Time,
) (model.TrafficReportPeriod, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return model.TrafficReportPeriod{}, fmt.Errorf("load schedule timezone: %w", err)
	}
	localRun := runAt.In(location)
	localEnd := localMidnight(localRun)
	var localStart time.Time
	switch schedule.Cadence {
	case model.TrafficReportDaily:
		localStart = localEnd.AddDate(0, 0, -1)
	case model.TrafficReportWeekly:
		localStart = localEnd.AddDate(0, 0, -7)
	case model.TrafficReportMonthly:
		localEnd = time.Date(localRun.Year(), localRun.Month(), 1, 0, 0, 0, 0, location)
		localStart = localEnd.AddDate(0, -1, 0)
	default:
		return model.TrafficReportPeriod{}, fmt.Errorf("invalid report cadence")
	}
	return model.TrafficReportPeriod{
		Start: localStart.UTC(), End: localEnd.UTC(),
		Key: schedule.Cadence + ":" + localStart.Format("2006-01-02"),
	}, nil
}

func weeklyCandidate(after time.Time, schedule model.TrafficReportSchedule) time.Time {
	weekday := int(after.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	days := (schedule.Weekday - weekday + 7) % 7
	candidate := localClock(after, schedule, days)
	if !candidate.After(after) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func monthlyCandidate(after time.Time, schedule model.TrafficReportSchedule) time.Time {
	candidate := localMonthClock(after.Year(), after.Month(), after.Location(), schedule)
	if !candidate.After(after) {
		nextMonth := time.Date(after.Year(), after.Month()+1, 1, 0, 0, 0, 0, after.Location())
		candidate = localMonthClock(nextMonth.Year(), nextMonth.Month(), after.Location(), schedule)
	}
	return candidate
}

func localClock(after time.Time, schedule model.TrafficReportSchedule, days int) time.Time {
	date := after.AddDate(0, 0, days)
	return time.Date(
		date.Year(), date.Month(), date.Day(), schedule.SendHour, schedule.SendMinute, 0, 0,
		after.Location(),
	)
}

func localMonthClock(
	year int,
	month time.Month,
	location *time.Location,
	schedule model.TrafficReportSchedule,
) time.Time {
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	return time.Date(
		year, month, min(schedule.MonthDay, lastDay),
		schedule.SendHour, schedule.SendMinute, 0, 0, location,
	)
}

func localMidnight(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}
