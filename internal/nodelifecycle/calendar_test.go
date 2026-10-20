package nodelifecycle

import (
	"math"
	"testing"
	"time"
)

func TestCalendarAnchorsAndAdvancement(t *testing.T) {
	cases := []struct {
		date, unit string
		count      int
		expected   string
	}{{"2024-01-31", "month", 1, "2024-02-29"}, {"2023-01-31", "month", 1, "2023-02-28"}, {"2024-02-29", "year", 1, "2025-02-28"}, {"2024-02-29", "year", 4, "2028-02-29"}, {"2024-03-09", "day", 1, "2024-03-10"}, {"2024-12-31", "month", 2, "2025-02-28"}}
	for _, test := range cases {
		t.Run(test.date+test.unit, func(t *testing.T) {
			anchor, err := AnchorForDate(test.date)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := AdvanceCalendar(CalendarPeriod{ExpiresOn: test.date, Unit: test.unit, Count: test.count, Anchor: anchor})
			if err != nil || actual != test.expected {
				t.Fatal(test.expected, actual, err)
			}
		})
	}
	anchor, err := AnchorForDate("2024-01-30")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := AdvanceCalendar(CalendarPeriod{ExpiresOn: "2024-02-29", Unit: "month", Count: 1, Anchor: anchor})
	if err != nil || actual != "2024-03-30" {
		t.Fatal(actual, err)
	}
}

func TestCalendarLimitsInvalidAndLongDowntime(t *testing.T) {
	anchor, _ := AnchorForDate("2024-01-31")
	period := CalendarPeriod{ExpiresOn: "2024-01-31", Unit: "month", Count: 1, Anchor: anchor}
	result, err := AdvanceUntilFuture(period, "2024-04-30")
	if err != nil || result.ExpiresOn != "2024-05-31" || result.Intervals != 4 {
		t.Fatal(result, err)
	}
	if _, err = AdvanceUntilFuture(period, "2035-01-01"); err != ErrAdvanceLimit {
		t.Fatal(err)
	}
	if _, err = AdvanceUntilFuture(period, "bad"); err != ErrInvalid {
		t.Fatal(err)
	}
	period.ExpiresOn = "bad"
	if _, err = AdvanceUntilFuture(period, "2024-01-01"); err != ErrInvalid {
		t.Fatal(err)
	}
	for _, value := range []string{"2023-02-29", "0000-01-01", "2024-1-1", "2024-01-01T00:00:00Z"} {
		if _, err = AnchorForDate(value); err != ErrInvalid {
			t.Fatal(value, err)
		}
	}
	for _, bad := range []CalendarPeriod{{ExpiresOn: "bad"}, {ExpiresOn: "2024-01-01", Count: 0}, {ExpiresOn: "2024-01-01", Count: 1}, {ExpiresOn: "2024-01-01", Count: 1, Anchor: anchor, Unit: "bad"}, {ExpiresOn: "9999-12-31", Count: 1, Anchor: anchor, Unit: "year"}} {
		if _, err = AdvanceCalendar(bad); err != ErrInvalid {
			t.Fatal(bad, err)
		}
	}
}

func TestLifecycleStatusUsesSiteDateAcrossDST(t *testing.T) {
	now := time.Date(2024, 3, 10, 4, 30, 0, 0, time.UTC)
	status, days, err := LifecycleStatus("2024-03-10", now, "America/New_York")
	if err != nil || status != "scheduled" || *days != 1 {
		t.Fatal(status, days, err)
	}
	for _, test := range []struct {
		expiry, status string
		days           int
	}{{"2024-03-10", "due_today", 0}, {"2024-03-09", "expired", -1}, {"2024-03-11", "scheduled", 1}} {
		actual, remaining, err := LifecycleStatus(test.expiry, now, "UTC")
		if err != nil || actual != test.status || *remaining != test.days {
			t.Fatal(actual, remaining, err)
		}
	}
	if status, days, err = LifecycleStatus("", now, "UTC"); err != nil || status != "untracked" || days != nil {
		t.Fatal(status, days, err)
	}
	if _, _, err = LifecycleStatus("bad", now, "UTC"); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, _, err = LifecycleStatus("", now, "not/a-zone"); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, days, err = LifecycleStatus("9999-12-31", now, "UTC"); err != nil || *days < 2000000 {
		t.Fatal("date duration overflow", days, err)
	}
}

func TestExactPricesNeverUseFloatingPoint(t *testing.T) {
	for input, want := range map[string]int64{"0": 0, "1.2": 1200000, "0.000001": 1, "123.450000": 123450000, "9223372036854.775807": math.MaxInt64} {
		actual, err := ParsePrice(input)
		if err != nil || actual != want {
			t.Fatal(input, actual, err)
		}
		formatted, err := FormatPrice(actual)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, err := ParsePrice(formatted)
		if err != nil || roundtrip != actual {
			t.Fatal(formatted, err)
		}
	}
	for _, input := range []string{"", "-1", "+1", "1e3", ".1", "1.", "1.0000001", "1.2.3", "9223372036854.775808", "9223372036855", "999999999999999999999999"} {
		if _, err := ParsePrice(input); err != ErrInvalid {
			t.Fatal(input, err)
		}
	}
	if _, err := FormatPrice(-1); err != ErrInvalid {
		t.Fatal(err)
	}
}
