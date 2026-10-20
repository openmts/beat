package model

import (
	"errors"
	"testing"
)

func dashboardWithTimeOverride(from, to string) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].TimeFrom = from
	definition.Panels[0].TimeTo = to
	return definition
}

func TestDashboardPanelTimeOverrideAcceptsBoundedWindows(t *testing.T) {
	cases := map[string][2]string{
		"inherit":        {"", ""},
		"start only":     {"now-30m", ""},
		"explicit now":   {"now-6h", "now"},
		"minimum window": {"now-1m", "now"},
		"maximum window": {"now-31d", "now"},
		"inner window":   {"now-1d", "now-1h"},
	}
	for name, tokens := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(dashboardWithTimeOverride(tokens[0], tokens[1])); err != nil {
				t.Fatalf("valid panel time override rejected: %v", err)
			}
		})
	}
}

func TestDashboardPanelTimeOverrideRejectsInvalidWindows(t *testing.T) {
	cases := map[string][2]string{
		"end without start": {"", "now"},
		"missing prefix":    {"30m", ""},
		"unknown unit":      {"now-30x", ""},
		"too short":         {"now-30s", "now"},
		"too long":          {"now-40d", "now"},
		"reversed window":   {"now-1h", "now-2h"},
		"too many digits":   {"now-12345m", ""},
	}
	for name, tokens := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateDashboard(dashboardWithTimeOverride(tokens[0], tokens[1]))
			if !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid panel time override accepted: %v", err)
			}
		})
	}
}

func TestDashboardTimeTokenSeconds(t *testing.T) {
	valid := map[string]int{"now": 0, "now-45s": 45, "now-30m": 1800, "now-6h": 21600, "now-2d": 172800}
	for token, expected := range valid {
		actual, ok := dashboardTimeTokenSeconds(token)
		if !ok || actual != expected {
			t.Fatalf("token %s resolved to (%d, %t), want (%d, true)", token, actual, ok, expected)
		}
	}
	for _, token := range []string{"", "now-", "NOW-5m", "now-5", "now-m", "now-1w", "now-99999m"} {
		if _, ok := dashboardTimeTokenSeconds(token); ok {
			t.Fatalf("token %q must not resolve", token)
		}
	}
}

func TestDashboardPanelTimeShift(t *testing.T) {
	for _, shift := range []string{"", "1s", "30m", "12h", "1d", "31d"} {
		if err := ValidateDashboard(dashboardWithTimeOverride("", "")); err != nil {
			t.Fatal(err)
		}
		definition := dashboardWithTimeOverride("now-1h", "now")
		definition.Panels[0].TimeShift = shift
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("valid time shift %q rejected: %v", shift, err)
		}
	}
	for _, shift := range []string{"1w", "0s", "32d", "now-1d", "1", "-1d", "99999m"} {
		definition := dashboardWithTimeOverride("", "")
		definition.Panels[0].TimeShift = shift
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("time shift %q accepted: %v", shift, err)
		}
	}
}
