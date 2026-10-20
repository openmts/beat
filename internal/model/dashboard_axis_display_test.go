package model

import (
	"errors"
	"testing"
)

func TestValidateDashboardAxisDisplay(t *testing.T) {
	boolean := func(value bool) *bool { return &value }
	pointFloat := func(value float64) *float64 { return &value }

	t.Run("accepts the display options on time series and bar charts", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart"} {
			for _, placement := range []string{"", DashboardAxisPlacementLeft, DashboardAxisPlacementRight} {
				for _, grid := range []*bool{nil, boolean(true), boolean(false)} {
					for _, centered := range []bool{false, true} {
						panel := legendPanel(panelType, true)
						panel.AxisPlacement, panel.Grid, panel.CenteredZero = placement, grid, centered
						if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
							t.Fatalf("%s %q grid=%v centered=%v rejected: %v", panelType, placement, grid, centered, err)
						}
					}
				}
			}
		}
	})

	t.Run("rejects unknown placements and misplaced display options", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"unknown placement": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.AxisPlacement = "auto"
				return panel
			}(), "invalid metric dashboard: axis placement must be left or right"},
			"grid on a stat panel": {func() DashboardPanel {
				panel := legendPanel("stat", true)
				panel.Grid = boolean(false)
				return panel
			}(), "invalid metric dashboard: axis display options are only available on time series and bar chart panels"},
			"centered zero with a fixed minimum": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.CenteredZero, panel.Min = true, pointFloat(0)
				return panel
			}(), "invalid metric dashboard: centered zero needs an automatic axis without fixed limits"},
			"centered zero with a fixed maximum": {func() DashboardPanel {
				panel := legendPanel("barchart", true)
				panel.CenteredZero, panel.Max = true, pointFloat(100)
				return panel
			}(), "invalid metric dashboard: centered zero needs an automatic axis without fixed limits"},
			"centered zero on a stat panel": {func() DashboardPanel {
				panel := legendPanel("stat", true)
				panel.CenteredZero = true
				return panel
			}(), "invalid metric dashboard: axis display options are only available on time series and bar chart panels"},
			"placement on a table": {func() DashboardPanel {
				panel := legendPanel("table", true)
				panel.AxisPlacement = DashboardAxisPlacementRight
				return panel
			}(), "invalid metric dashboard: axis display options are only available on time series and bar chart panels"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})
}
