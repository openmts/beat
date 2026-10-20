package model

import (
	"errors"
	"math"
	"testing"
)

func TestValidateDashboardSoftAxes(t *testing.T) {
	point := func(value float64) *float64 { return &value }

	t.Run("accepts the documented limits on time series and bar charts", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart"} {
			panel := legendPanel(panelType, true)
			panel.SoftMin, panel.SoftMax = point(-10), point(90)
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("%s rejected both soft limits: %v", panelType, err)
			}
			panel.SoftMin, panel.SoftMax = point(-10), nil
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("%s rejected a lone soft minimum: %v", panelType, err)
			}
			panel.SoftMin, panel.SoftMax = nil, point(90)
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("%s rejected a lone soft maximum: %v", panelType, err)
			}
		}
	})

	t.Run("accepts a soft limit when the fixed limit sits on the other side", func(t *testing.T) {
		panel := legendPanel("timeseries", true)
		panel.Min, panel.SoftMin, panel.SoftMax = point(0), nil, point(90)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("soft maximum next to a fixed minimum rejected: %v", err)
		}
	})

	t.Run("rejects invalid combinations", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"not a number": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.SoftMin = point(math.NaN())
				return panel
			}(), "invalid metric dashboard: soft axis limits must be finite"},
			"infinite": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.SoftMax = point(math.Inf(1))
				return panel
			}(), "invalid metric dashboard: soft axis limits must be finite"},
			"reversed order": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.SoftMin, panel.SoftMax = point(90), point(10)
				return panel
			}(), "invalid metric dashboard: soft axis minimum must be below maximum"},
			"equal bounds": {func() DashboardPanel {
				panel := legendPanel("barchart", true)
				panel.SoftMin, panel.SoftMax = point(10), point(10)
				return panel
			}(), "invalid metric dashboard: soft axis minimum must be below maximum"},
			"minimum conflict": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.Min, panel.SoftMin = point(0), point(-10)
				return panel
			}(), "invalid metric dashboard: soft axis limits need an automatic axis limit on the same side"},
			"maximum conflict": {func() DashboardPanel {
				panel := legendPanel("barchart", true)
				panel.Max, panel.SoftMax = point(100), point(90)
				return panel
			}(), "invalid metric dashboard: soft axis limits need an automatic axis limit on the same side"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})

	t.Run("rejects the limits on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"stat", "gauge", "bargauge", "piechart", "heatmap", "histogram", "geomap", "statetimeline", "table"} {
			for _, change := range []func(*DashboardPanel){
				func(panel *DashboardPanel) { panel.SoftMin = point(-10) },
				func(panel *DashboardPanel) { panel.SoftMax = point(90) },
			} {
				panel := legendPanel(panelType, true)
				change(&panel)
				err := validateDashboardPanel(panel, map[string]bool{})
				want := "invalid metric dashboard: soft axis limits are only available on time series and bar chart panels"
				if !errors.Is(err, ErrInvalidDashboard) || err.Error() != want {
					t.Fatalf("%s: unexpected error %v", panelType, err)
				}
			}
		}
	})
}
