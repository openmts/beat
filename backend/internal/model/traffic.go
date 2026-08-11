package model

import (
	"fmt"
	"math"
	"time"
)

const (
	TrafficLimitUp   = "up"
	TrafficLimitDown = "down"
	TrafficLimitSum  = "sum"
	TrafficLimitMin  = "min"
	TrafficLimitMax  = "max"

	TrafficStatusUnlimited = "unlimited"
	TrafficStatusNormal    = "normal"
	TrafficStatusWarning   = "warning"
	TrafficStatusCritical  = "critical"
	TrafficStatusExceeded  = "exceeded"
)

type TrafficPolicy struct {
	Limit     int64
	LimitType string
	ResetDay  int
}

type TrafficTotals struct {
	Sent         float64
	Received     float64
	TrackedSince *time.Time
}

type TrafficSummary struct {
	Sent         float64    `json:"sent"`
	Received     float64    `json:"received"`
	Used         float64    `json:"used"`
	Limit        int64      `json:"limit"`
	Remaining    *float64   `json:"remaining"`
	Percentage   *float64   `json:"percentage"`
	LimitType    string     `json:"limit_type"`
	ResetDay     int        `json:"reset_day"`
	PeriodStart  time.Time  `json:"period_start"`
	NextReset    time.Time  `json:"next_reset"`
	TrackedSince *time.Time `json:"tracked_since"`
	Status       string     `json:"status"`
}

func (policy TrafficPolicy) Validate() error {
	if policy.Limit < 0 {
		return fmt.Errorf("traffic limit must be non-negative")
	}
	if !isTrafficLimitType(policy.LimitType) {
		return fmt.Errorf("invalid traffic limit type")
	}
	if policy.ResetDay < 1 || policy.ResetDay > 31 {
		return fmt.Errorf("traffic reset day must be between 1 and 31")
	}
	return nil
}

func BillingPeriod(now time.Time, resetDay int) (time.Time, time.Time) {
	now = now.UTC()
	currentReset := resetDate(now.Year(), now.Month(), resetDay)
	if now.Before(currentReset) {
		previousYear, previousMonth := shiftMonth(now.Year(), now.Month(), -1)
		return resetDate(previousYear, previousMonth, resetDay), currentReset
	}
	nextYear, nextMonth := shiftMonth(now.Year(), now.Month(), 1)
	return currentReset, resetDate(nextYear, nextMonth, resetDay)
}

func SummarizeTraffic(
	policy TrafficPolicy,
	totals TrafficTotals,
	now time.Time,
) (TrafficSummary, error) {
	if err := policy.Validate(); err != nil {
		return TrafficSummary{}, err
	}
	if !validTrafficTotal(totals.Sent) || !validTrafficTotal(totals.Received) {
		return TrafficSummary{}, fmt.Errorf("traffic totals must be finite non-negative numbers")
	}
	start, next := BillingPeriod(now, policy.ResetDay)
	used := trafficUsed(policy.LimitType, totals)
	summary := TrafficSummary{
		Sent: totals.Sent, Received: totals.Received, Used: used, Limit: policy.Limit,
		LimitType: policy.LimitType, ResetDay: policy.ResetDay,
		PeriodStart: start, NextReset: next, TrackedSince: totals.TrackedSince,
		Status: TrafficStatusUnlimited,
	}
	if policy.Limit == 0 {
		return summary, nil
	}
	limit := float64(policy.Limit)
	remaining := math.Max(0, limit-used)
	percentage := used / limit * 100
	summary.Remaining = &remaining
	summary.Percentage = &percentage
	summary.Status = trafficStatus(percentage)
	return summary, nil
}

func resetDate(year int, month time.Month, resetDay int) time.Time {
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(resetDay, lastDay), 0, 0, 0, 0, time.UTC)
}

func shiftMonth(year int, month time.Month, offset int) (int, time.Month) {
	shifted := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, offset, 0)
	return shifted.Year(), shifted.Month()
}

func isTrafficLimitType(limitType string) bool {
	switch limitType {
	case TrafficLimitUp, TrafficLimitDown, TrafficLimitSum, TrafficLimitMin, TrafficLimitMax:
		return true
	default:
		return false
	}
}

func trafficUsed(limitType string, totals TrafficTotals) float64 {
	switch limitType {
	case TrafficLimitUp:
		return totals.Sent
	case TrafficLimitDown:
		return totals.Received
	case TrafficLimitMin:
		return math.Min(totals.Sent, totals.Received)
	case TrafficLimitMax:
		return math.Max(totals.Sent, totals.Received)
	default:
		return totals.Sent + totals.Received
	}
}

func trafficStatus(percentage float64) string {
	switch {
	case percentage >= 100:
		return TrafficStatusExceeded
	case percentage >= 90:
		return TrafficStatusCritical
	case percentage >= 70:
		return TrafficStatusWarning
	default:
		return TrafficStatusNormal
	}
}

func validTrafficTotal(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
