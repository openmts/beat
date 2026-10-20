package model

import (
	"errors"
	"testing"
)

func TestValidateDashboardBarChart(t *testing.T) {
	point := func(value int) *int { return &value }

	t.Run("accepts the documented bounds on bar charts", func(t *testing.T) {
		panel := legendPanel("barchart", false)
		panel.BarWidth, panel.BarRadius = point(MinDashboardBarWidth), point(MinDashboardBarRadius)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("lower bounds rejected: %v", err)
		}
		panel.BarWidth, panel.BarRadius = point(MaxDashboardBarWidth), point(MaxDashboardBarRadius)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("upper bounds rejected: %v", err)
		}
	})

	t.Run("rejects out-of-range values", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"bar width below": {func() DashboardPanel {
				panel := legendPanel("barchart", false)
				panel.BarWidth = point(MinDashboardBarWidth - 1)
				return panel
			}(), "invalid metric dashboard: bar width must be between 4 and 40"},
			"bar width above": {func() DashboardPanel {
				panel := legendPanel("barchart", false)
				panel.BarWidth = point(MaxDashboardBarWidth + 1)
				return panel
			}(), "invalid metric dashboard: bar width must be between 4 and 40"},
			"bar radius below": {func() DashboardPanel {
				panel := legendPanel("barchart", false)
				panel.BarRadius = point(MinDashboardBarRadius - 1)
				return panel
			}(), "invalid metric dashboard: bar radius must be between 0 and 10"},
			"bar radius above": {func() DashboardPanel {
				panel := legendPanel("barchart", false)
				panel.BarRadius = point(MaxDashboardBarRadius + 1)
				return panel
			}(), "invalid metric dashboard: bar radius must be between 0 and 10"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})

	t.Run("rejects the options on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "bargauge", "stat", "piechart"} {
			for _, change := range []func(*DashboardPanel){
				func(panel *DashboardPanel) { panel.BarWidth = point(DefaultDashboardBarWidth) },
				func(panel *DashboardPanel) { panel.BarRadius = point(4) },
			} {
				panel := legendPanel(panelType, false)
				change(&panel)
				err := validateDashboardPanel(panel, map[string]bool{})
				if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: bar chart options are only available on bar chart panels" {
					t.Fatalf("%s: unexpected error %v", panelType, err)
				}
			}
		}
	})

	t.Run("keeps panels without the fields valid", func(t *testing.T) {
		if err := validateDashboardPanel(legendPanel("timeseries", false), map[string]bool{}); err != nil {
			t.Fatalf("panel without bar chart options rejected: %v", err)
		}
	})
}
