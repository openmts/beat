package model

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDashboardAxisLabel(t *testing.T) {
	t.Run("accepts a bounded label on time series and bar charts", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart"} {
			for _, label := range []string{"", "CPU", "已用内存（字节）", strings.Repeat("a", MaxDashboardAxisLabelBytes)} {
				panel := legendPanel(panelType, true)
				panel.AxisLabel = label
				if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
					t.Fatalf("%s %q rejected: %v", panelType, label, err)
				}
			}
		}
	})

	t.Run("rejects over-long, invalid and misplaced labels", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"over the byte budget": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.AxisLabel = strings.Repeat("a", MaxDashboardAxisLabelBytes+1)
				return panel
			}(), "invalid metric dashboard: axis label is invalid"},
			"nul character": {func() DashboardPanel {
				panel := legendPanel("barchart", true)
				panel.AxisLabel = "CPU\x00"
				return panel
			}(), "invalid metric dashboard: axis label is invalid"},
			"newline": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.AxisLabel = "CPU\nusage"
				return panel
			}(), "invalid metric dashboard: axis label is invalid"},
			"tab": {func() DashboardPanel {
				panel := legendPanel("timeseries", true)
				panel.AxisLabel = "CPU\tusage"
				return panel
			}(), "invalid metric dashboard: axis label is invalid"},
			"other panel type": {func() DashboardPanel {
				panel := legendPanel("stat", true)
				panel.AxisLabel = "CPU"
				return panel
			}(), "invalid metric dashboard: axis label is only available on time series and bar chart panels"},
			"empty label stays optional": {func() DashboardPanel {
				panel := legendPanel("table", true)
				panel.AxisLabel = ""
				return panel
			}(), ""},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if test.want == "" {
				if err != nil {
					t.Fatalf("%s: unexpected error %v", name, err)
				}
				continue
			}
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})
}
