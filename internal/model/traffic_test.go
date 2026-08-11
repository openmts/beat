package model

import (
	"math"
	"testing"
	"time"
)

func TestBillingPeriod(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		resetDay  int
		wantStart time.Time
		wantNext  time.Time
	}{
		{
			name:      "after reset day",
			now:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.FixedZone("local", 8*60*60)),
			resetDay:  1,
			wantStart: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "before reset day",
			now:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
			resetDay:  20,
			wantStart: time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "short month clamps day 31",
			now:       time.Date(2025, time.February, 28, 12, 0, 0, 0, time.UTC),
			resetDay:  31,
			wantStart: time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2025, time.March, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "leap year clamps day 31",
			now:       time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC),
			resetDay:  31,
			wantStart: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "before day 31 after shorter previous month",
			now:       time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
			resetDay:  31,
			wantStart: time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "day 31 advances into shorter next month",
			now:       time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
			resetDay:  31,
			wantStart: time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC),
			wantNext:  time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, next := BillingPeriod(test.now, test.resetDay)
			if !start.Equal(test.wantStart) || !next.Equal(test.wantNext) {
				t.Fatalf("period = %s to %s, want %s to %s", start, next, test.wantStart, test.wantNext)
			}
		})
	}
}

func TestTrafficPolicyValidate(t *testing.T) {
	valid := []TrafficPolicy{
		{Limit: 0, LimitType: TrafficLimitSum, ResetDay: 1},
		{Limit: 1, LimitType: TrafficLimitUp, ResetDay: 31},
		{Limit: 1, LimitType: TrafficLimitDown, ResetDay: 15},
		{Limit: 1, LimitType: TrafficLimitMin, ResetDay: 15},
		{Limit: 1, LimitType: TrafficLimitMax, ResetDay: 15},
	}
	for _, policy := range valid {
		if err := policy.Validate(); err != nil {
			t.Fatalf("valid policy %#v: %v", policy, err)
		}
	}
	invalid := []TrafficPolicy{
		{Limit: -1, LimitType: TrafficLimitSum, ResetDay: 1},
		{Limit: 1, LimitType: "invalid", ResetDay: 1},
		{Limit: 1, LimitType: TrafficLimitSum, ResetDay: 0},
		{Limit: 1, LimitType: TrafficLimitSum, ResetDay: 32},
	}
	for _, policy := range invalid {
		if err := policy.Validate(); err == nil {
			t.Fatalf("expected invalid policy %#v", policy)
		}
	}
}

func TestSummarizeTraffic(t *testing.T) {
	now := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		policy     TrafficPolicy
		wantUsed   float64
		wantStatus string
	}{
		{"upload", TrafficPolicy{Limit: 1000, LimitType: TrafficLimitUp, ResetDay: 1}, 400, TrafficStatusNormal},
		{"download", TrafficPolicy{Limit: 1000, LimitType: TrafficLimitDown, ResetDay: 1}, 600, TrafficStatusNormal},
		{"sum warning", TrafficPolicy{Limit: 1000, LimitType: TrafficLimitSum, ResetDay: 1}, 1000, TrafficStatusExceeded},
		{"minimum", TrafficPolicy{Limit: 500, LimitType: TrafficLimitMin, ResetDay: 1}, 400, TrafficStatusWarning},
		{"maximum", TrafficPolicy{Limit: 600, LimitType: TrafficLimitMax, ResetDay: 1}, 600, TrafficStatusExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary, err := SummarizeTraffic(test.policy, TrafficTotals{Sent: 400, Received: 600}, now)
			if err != nil {
				t.Fatalf("summarize traffic: %v", err)
			}
			if summary.Used != test.wantUsed || summary.Status != test.wantStatus {
				t.Fatalf("summary = %#v, want used %v status %s", summary, test.wantUsed, test.wantStatus)
			}
		})
	}
}

func TestSummarizeTrafficStatusAndUnlimited(t *testing.T) {
	now := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		used       float64
		wantStatus string
	}{
		{699, TrafficStatusNormal},
		{700, TrafficStatusWarning},
		{900, TrafficStatusCritical},
		{1000, TrafficStatusExceeded},
	}
	for _, test := range tests {
		summary, err := SummarizeTraffic(
			TrafficPolicy{Limit: 1000, LimitType: TrafficLimitSum, ResetDay: 1},
			TrafficTotals{Sent: test.used, Received: 0},
			now,
		)
		if err != nil {
			t.Fatalf("summarize status: %v", err)
		}
		if summary.Status != test.wantStatus {
			t.Fatalf("status for %v = %s, want %s", test.used, summary.Status, test.wantStatus)
		}
		if summary.Remaining == nil || summary.Percentage == nil || math.IsNaN(*summary.Percentage) {
			t.Fatalf("limited summary values = %#v", summary)
		}
	}

	unlimited, err := SummarizeTraffic(
		TrafficPolicy{Limit: 0, LimitType: TrafficLimitSum, ResetDay: 1},
		TrafficTotals{Sent: 10, Received: 20},
		now,
	)
	if err != nil {
		t.Fatalf("summarize unlimited: %v", err)
	}
	if unlimited.Status != TrafficStatusUnlimited || unlimited.Remaining != nil || unlimited.Percentage != nil {
		t.Fatalf("unlimited summary = %#v", unlimited)
	}
}
