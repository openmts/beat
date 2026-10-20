package model

import (
	"errors"
	"testing"
)

func legendPanel(panelType string, legend bool) DashboardPanel {
	return DashboardPanel{
		ID: "panel-1", Title: "CPU", Type: panelType, Size: "medium", Legend: legend,
		Thresholds: []DashboardThreshold{},
		Series:     []DashboardSeries{{RefID: "A", NodeID: "node-1", Metric: "cpu", Aggregation: "avg", Color: "blue"}},
	}
}

func TestValidateDashboardLegendPlacement(t *testing.T) {
	t.Run("accepts the documented placements on panels that draw a legend", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart"} {
			for _, placement := range []string{"", "bottom", "right"} {
				panel := legendPanel(panelType, true)
				panel.LegendPlacement = placement
				if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
					t.Fatalf("%s %q rejected: %v", panelType, placement, err)
				}
			}
		}
	})

	t.Run("rejects unknown placements, hidden legends and other panel types", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"unknown value": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.LegendPlacement = "top"
				return panel
			}(), "invalid metric dashboard: legend placement is invalid"},
			"hidden legend": {func() DashboardPanel {
				panel := legendPanel("timeseries", false)
				panel.LegendPlacement = "right"
				return panel
			}(), "invalid metric dashboard: legend placement requires a visible legend"},
			"stat panel": {func() DashboardPanel {
				panel := legendPanel("stat", true)
				panel.LegendPlacement = "right"
				return panel
			}(), "invalid metric dashboard: legend placement is only available on timeseries and bar chart panels"},
			"text panel": {func() DashboardPanel {
				panel := legendPanel("text", true)
				panel.LegendPlacement = "bottom"
				return panel
			}(), "invalid metric dashboard: legend placement is only available on timeseries and bar chart panels"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})

	t.Run("keeps panels without the field valid", func(t *testing.T) {
		if err := validateDashboardPanel(legendPanel("timeseries", false), map[string]bool{}); err != nil {
			t.Fatalf("panel without the field rejected: %v", err)
		}
	})
}
