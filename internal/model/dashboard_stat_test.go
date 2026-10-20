package model

import (
	"errors"
	"testing"
)

func statStylePanel(panelType string) DashboardPanel {
	panel := legendPanel(panelType, false)
	if panelType == "stat" || panelType == "gauge" {
		panel.Series = panel.Series[:1]
	}
	return panel
}

func TestValidateDashboardStatOptions(t *testing.T) {
	t.Run("accepts the documented values on stat panels", func(t *testing.T) {
		for _, color := range []string{"", "value", "background", "none"} {
			for _, text := range []string{"", "auto", "value", "name", "none"} {
				for _, size := range []string{"", "auto", "sm", "md", "lg", "xl"} {
					for _, orientation := range []string{"", "auto", "horizontal", "vertical"} {
						panel := statStylePanel("stat")
						panel.StatColorMode, panel.StatTextMode, panel.StatTextSize = color, text, size
						panel.StatOrientation = orientation
						if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
							t.Fatalf("color %q text %q size %q orientation %q rejected: %v", color, text, size, orientation, err)
						}
					}
				}
			}
		}
	})

	t.Run("rejects unknown values", func(t *testing.T) {
		cases := map[string]struct {
			panel DashboardPanel
			want  string
		}{
			"color mode": {func() DashboardPanel {
				panel := statStylePanel("stat")
				panel.StatColorMode = "gradient"
				return panel
			}(), "invalid metric dashboard: stat color mode is invalid"},
			"text mode": {func() DashboardPanel {
				panel := statStylePanel("stat")
				panel.StatTextMode = "labels"
				return panel
			}(), "invalid metric dashboard: stat text mode is invalid"},
			"text size": {func() DashboardPanel {
				panel := statStylePanel("stat")
				panel.StatTextSize = "xxl"
				return panel
			}(), "invalid metric dashboard: stat text size is invalid"},
			"orientation": {func() DashboardPanel {
				panel := statStylePanel("stat")
				panel.StatOrientation = "diagonal"
				return panel
			}(), "invalid metric dashboard: stat orientation is invalid"},
		}
		for name, test := range cases {
			err := validateDashboardPanel(test.panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != test.want {
				t.Fatalf("%s: unexpected error %v", name, err)
			}
		}
	})

	t.Run("rejects the styles on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "barchart", "gauge", "table", "text"} {
			for _, change := range []func(*DashboardPanel){
				func(panel *DashboardPanel) { panel.StatColorMode = "background" },
				func(panel *DashboardPanel) { panel.StatTextMode = "value" },
				func(panel *DashboardPanel) { panel.StatTextSize = "xl" },
				func(panel *DashboardPanel) { panel.StatOrientation = "vertical" },
			} {
				panel := statStylePanel(panelType)
				panel.Legend = false
				change(&panel)
				err := validateDashboardPanel(panel, map[string]bool{})
				if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: stat styles are only available on stat panels" {
					t.Fatalf("%s: unexpected error %v", panelType, err)
				}
			}
		}
	})

	t.Run("keeps panels without the fields valid", func(t *testing.T) {
		if err := validateDashboardPanel(statStylePanel("stat"), map[string]bool{}); err != nil {
			t.Fatalf("stat panel without the styles rejected: %v", err)
		}
		if err := validateDashboardPanel(statStylePanel("timeseries"), map[string]bool{}); err != nil {
			t.Fatalf("timeseries panel without the styles rejected: %v", err)
		}
	})
}
