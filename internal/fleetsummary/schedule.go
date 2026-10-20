package fleetsummary

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	// localScanMinutes 覆盖最长 25 小时的本地日，用于解析 DST 间隙与重复时刻。
	localScanMinutes = 26 * 60
)

// ValidateSchedule 校验计划名称、节奏、时区、本地时间、阈值与渠道作用域。
func ValidateSchedule(schedule model.FleetSummarySchedule) error {
	name := strings.TrimSpace(schedule.Name)
	if name == "" || len(name) > 100 {
		return fmt.Errorf("schedule name must be 1-100 characters")
	}
	if _, _, err := parseLocalTime(schedule.LocalTime); err != nil {
		return err
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return fmt.Errorf("load schedule timezone: %w", err)
	}
	if err := validateCadence(schedule); err != nil {
		return err
	}
	if schedule.NameThreshold < 0 || schedule.NameThreshold > model.FleetSummaryMaximumNameThreshold {
		return fmt.Errorf("name threshold must be between 0 and %d", model.FleetSummaryMaximumNameThreshold)
	}
	if len(schedule.ChannelIDs) == 0 {
		return fmt.Errorf("at least one notification channel is required")
	}
	return nil
}

func validateCadence(schedule model.FleetSummarySchedule) error {
	switch schedule.Cadence {
	case model.FleetSummaryDaily:
		if schedule.Weekday != nil {
			return fmt.Errorf("daily schedules must not set a weekday")
		}
	case model.FleetSummaryWeekly:
		if schedule.Weekday == nil || *schedule.Weekday < 0 || *schedule.Weekday > 6 {
			return fmt.Errorf("weekly schedules require a weekday between 0 and 6")
		}
	default:
		return fmt.Errorf("invalid summary cadence")
	}
	return nil
}

func parseLocalTime(value string) (int, int, error) {
	hour, minute, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return 0, 0, fmt.Errorf("local time must use HH:MM")
	}
	parsedHour, err := strconv.Atoi(hour)
	if err != nil {
		return 0, 0, fmt.Errorf("local time must use HH:MM")
	}
	parsedMinute, err := strconv.Atoi(minute)
	if err != nil || len(hour) != 2 || len(minute) != 2 ||
		parsedHour < 0 || parsedHour > 23 || parsedMinute < 0 || parsedMinute > 59 {
		return 0, 0, fmt.Errorf("local time must use HH:MM")
	}
	return parsedHour, parsedMinute, nil
}

// NextRun 返回严格晚于 after 的下一次本地计划时刻。
func NextRun(schedule model.FleetSummarySchedule, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load schedule timezone: %w", err)
	}
	hour, minute, err := parseLocalTime(schedule.LocalTime)
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(location)
	var candidate time.Time
	switch schedule.Cadence {
	case model.FleetSummaryDaily:
		candidate = localInstant(location, local.Year(), local.Month(), local.Day(), hour, minute)
		if !candidate.After(after) {
			candidate = localInstant(location, local.Year(), local.Month(), local.Day()+1, hour, minute)
		}
	case model.FleetSummaryWeekly:
		candidate = weeklyInstant(location, local, *schedule.Weekday, hour, minute)
		if !candidate.After(after) {
			candidate = weeklyInstant(location, local.AddDate(0, 0, 7), *schedule.Weekday, hour, minute)
		}
	default:
		return time.Time{}, fmt.Errorf("invalid summary cadence")
	}
	return candidate.UTC(), nil
}

func weeklyInstant(
	location *time.Location,
	local time.Time,
	weekday int,
	hour int,
	minute int,
) time.Time {
	day := local.AddDate(0, 0, weekdayShift(local.Weekday(), weekday))
	return localInstant(location, day.Year(), day.Month(), day.Day(), hour, minute)
}

// weekdayShift 返回从当前星期到目标星期需要推进的天数。
func weekdayShift(current time.Weekday, target int) int {
	return (target - int(current) + 7) % 7
}

// localInstant 返回该本地日期下第一个墙钟时间不早于 hour:minute 的绝对瞬间。
//
// DST 重复时刻取第一次出现；因 DST 跳过而不存在的本地时刻推进到当日第一个有效瞬间。
func localInstant(location *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	for offset := 0; offset <= localScanMinutes; offset++ {
		candidate := start.Add(time.Duration(offset) * time.Minute)
		wall := candidate.In(location)
		if wall.Hour() > hour || (wall.Hour() == hour && wall.Minute() >= minute) {
			return candidate
		}
	}
	return start
}

// PeriodKey 返回该次运行的本地周期标识：每日用本地日历日期，每周用 ISO 周。
func PeriodKey(schedule model.FleetSummarySchedule, run time.Time) (string, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return "", fmt.Errorf("load schedule timezone: %w", err)
	}
	local := run.In(location)
	switch schedule.Cadence {
	case model.FleetSummaryDaily:
		return local.Format("2006-01-02"), nil
	case model.FleetSummaryWeekly:
		year, week := local.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week), nil
	default:
		return "", fmt.Errorf("invalid summary cadence")
	}
}
