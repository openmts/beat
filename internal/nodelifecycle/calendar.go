package nodelifecycle

import (
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid node lifecycle request")

var ErrAdvanceLimit = errors.New("lifecycle advancement exceeds 120 intervals")

const MaximumAdvanceIntervals = 120

type CalendarAnchor struct {
	Month, Day int
	EndOfMonth bool
}

type CalendarPeriod struct {
	ExpiresOn, Unit string
	Count           int
	Anchor          CalendarAnchor
}

type Advancement struct {
	ExpiresOn string
	Intervals int
}

func ParseDate(value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil || len(value) != 10 || date.Format(time.DateOnly) != value || date.Year() < 1 {
		return time.Time{}, ErrInvalid
	}
	return date, nil
}

func AnchorForDate(value string) (CalendarAnchor, error) {
	date, err := ParseDate(value)
	if err != nil {
		return CalendarAnchor{}, err
	}
	return CalendarAnchor{Month: int(date.Month()), Day: date.Day(), EndOfMonth: date.Day() == daysInMonth(date.Year(), date.Month())}, nil
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func AdvanceCalendar(period CalendarPeriod) (string, error) {
	date, err := ParseDate(period.ExpiresOn)
	if err != nil {
		return "", err
	}
	if !validPeriod(period) {
		return "", ErrInvalid
	}
	var next time.Time
	switch period.Unit {
	case "day":
		next = date.AddDate(0, 0, period.Count)
	case "month":
		month := time.Date(date.Year(), date.Month()+time.Month(period.Count), 1, 0, 0, 0, 0, time.UTC)
		next = anchoredDate(month.Year(), month.Month(), period.Anchor)
	case "year":
		next = anchoredDate(date.Year()+period.Count, time.Month(period.Anchor.Month), period.Anchor)
	default:
		return "", ErrInvalid
	}
	if next.Year() > 9999 || !next.After(date) {
		return "", ErrInvalid
	}
	return next.Format(time.DateOnly), nil
}

func anchoredDate(year int, month time.Month, anchor CalendarAnchor) time.Time {
	day := min(anchor.Day, daysInMonth(year, month))
	if anchor.EndOfMonth {
		day = daysInMonth(year, month)
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func AdvanceUntilFuture(period CalendarPeriod, today string) (Advancement, error) {
	if _, err := ParseDate(today); err != nil {
		return Advancement{}, err
	}
	if _, err := ParseDate(period.ExpiresOn); err != nil {
		return Advancement{}, err
	}
	result := Advancement{ExpiresOn: period.ExpiresOn}
	for result.ExpiresOn <= today {
		if result.Intervals >= MaximumAdvanceIntervals {
			return Advancement{}, ErrAdvanceLimit
		}
		period.ExpiresOn = result.ExpiresOn
		next, err := AdvanceCalendar(period)
		if err != nil {
			return Advancement{}, err
		}
		result.ExpiresOn = next
		result.Intervals++
	}
	return result, nil
}

func LifecycleStatus(expiresOn string, now time.Time, zone string) (string, *int, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return "", nil, ErrInvalid
	}
	if expiresOn == "" {
		return "untracked", nil, nil
	}
	date, err := ParseDate(expiresOn)
	if err != nil {
		return "", nil, err
	}
	// 使用日期重建UTC午夜计算日差，DST一天23/25小时不改变合同天数。
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	remaining := int((date.Unix() - today.Unix()) / 86400)
	status := "scheduled"
	if remaining == 0 {
		status = "due_today"
	}
	if remaining < 0 {
		status = "expired"
	}
	return status, &remaining, nil
}

func validPeriod(period CalendarPeriod) bool {
	return period.Count >= 1 && period.Count <= MaximumAdvanceIntervals && period.Anchor.Day >= 1 && period.Anchor.Day <= 31 && period.Anchor.Month >= 1 && period.Anchor.Month <= 12
}
