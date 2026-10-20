package model

import (
	"errors"
	"testing"
)

func TestValidateDashboardAxisScale(t *testing.T) {
	point := func(value float64) *float64 { return &value }

	t.Run("accepts linear and logarithmic scales on time series and bar charts", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart"} {
			for _, scale := range []string{"", DashboardAxisScaleLinear, DashboardAxisScaleLog} {
				panel := legendPanel(panelType, true)
				panel.Scale = scale
				if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
					t.Fatalf("%s %q rejected: %v", panelType, scale, err)
				}
			}
		}
	})

	t.Run("accepts a positive logarithmic minimum", func(t *testing.T) {
		panel := legendPanel("timeseries", true)
		panel.Scale, panel.Min, panel.SoftMin = DashboardAxisScaleLog, point(1), nil
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("positive minimum rejected: %v", err)
		}
		panel.Min, panel.SoftMin = nil, point(2)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("positive soft minimum rejected: %v", err)
		}
	})

	t.Run("rejects unknown scales and non-positive logarithmic minimums", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"unknown scale": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.Scale = "log2"
				return panel
			}(), "invalid metric dashboard: axis scale must be linear or log"},
			"zero minimum": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.Scale, panel.Min = DashboardAxisScaleLog, point(0)
				return panel
			}(), "invalid metric dashboard: logarithmic axis needs a minimum above zero"},
			"negative minimum": {func() DashboardPanel {
				panel := legendPanel("barchart", true)
				panel.Scale, panel.Min = DashboardAxisScaleLog, point(-1)
				return panel
			}(), "invalid metric dashboard: logarithmic axis needs a minimum above zero"},
			"negative soft minimum": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.Scale, panel.SoftMin = DashboardAxisScaleLog, point(-10)
				return panel
			}(), "invalid metric dashboard: logarithmic axis needs a minimum above zero"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})

	t.Run("rejects the scale on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"stat", "gauge", "bargauge", "piechart", "heatmap", "histogram", "geomap", "statetimeline", "table"} {
			for _, scale := range []string{DashboardAxisScaleLinear, DashboardAxisScaleLog} {
				panel := legendPanel(panelType, true)
				panel.Scale = scale
				err := validateDashboardPanel(panel, map[string]bool{})
				want := "invalid metric dashboard: axis scale is only available on time series and bar chart panels"
				if !errors.Is(err, ErrInvalidDashboard) || err.Error() != want {
					t.Fatalf("%s %q: unexpected error %v", panelType, scale, err)
				}
			}
		}
	})
}
