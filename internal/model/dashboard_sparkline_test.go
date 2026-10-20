package model

import (
	"errors"
	"testing"
)

func sparklinePanel(panelType string, sparkline *bool) DashboardPanel {
	return DashboardPanel{
		ID: "panel-1", Title: "CPU", Type: panelType, Size: "medium",
		Legend: true, Thresholds: []DashboardThreshold{}, Sparkline: sparkline,
		Series: []DashboardSeries{{RefID: "A", NodeID: "node-1", Metric: "cpu", Aggregation: "avg", Color: "blue"}},
	}
}

func TestValidateDashboardSparkline(t *testing.T) {
	enabled := true
	disabled := false

	t.Run("accepts stat panels with or without the switch", func(t *testing.T) {
		for _, panel := range []DashboardPanel{sparklinePanel("stat", &enabled), sparklinePanel("stat", &disabled), sparklinePanel("stat", nil)} {
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("stat panel rejected: %v", err)
			}
		}
	})

	t.Run("rejects the sparkline on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "gauge", "bargauge", "piechart", "table", "heatmap"} {
			panel := sparklinePanel(panelType, &enabled)
			err := validateDashboardPanel(panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: sparkline is only available on stat panels" {
				t.Fatalf("%s: unexpected error %v", panelType, err)
			}
		}
	})

	t.Run("rejects the sparkline on panels that do not query", func(t *testing.T) {
		for _, panel := range []DashboardPanel{
			{ID: "panel-1", Title: "Notes", Type: "text", Size: "medium", Thresholds: []DashboardThreshold{}, Sparkline: &enabled},
			{ID: "panel-1", Title: "Group", Type: "row", Size: "medium", Thresholds: []DashboardThreshold{}, Sparkline: &enabled},
		} {
			if !errors.Is(validateDashboardPanel(panel, map[string]bool{}), ErrInvalidDashboard) {
				t.Fatalf("%s panel accepted a sparkline", panel.Type)
			}
		}
	})

	t.Run("keeps existing dashboards valid through the omitted field", func(t *testing.T) {
		panel := sparklinePanel("timeseries", nil)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("panel without the field rejected: %v", err)
		}
	})
}

func sparklineOptionsPanel(panelType string, enabled bool) DashboardPanel {
	panel := sparklinePanel(panelType, &enabled)
	panel.SparklineLineWidth = new(MaxDashboardSparklineLineWidth)
	panel.SparklineFillOpacity = new(MaxDashboardSparklineFillOpacity)
	panel.SparklineGradientMode = "hue"
	return panel
}

func TestValidateDashboardSparklineOptions(t *testing.T) {
	t.Run("accepts the option range on an enabled stat sparkline", func(t *testing.T) {
		enabled := true
		for _, width := range []int{MinDashboardSparklineLineWidth, DefaultDashboardSparklineLineWidth, MaxDashboardSparklineLineWidth} {
			for _, fill := range []int{0, 40, MaxDashboardSparklineFillOpacity} {
				panel := sparklinePanel("stat", &enabled)
				panel.SparklineLineWidth = new(width)
				panel.SparklineFillOpacity = new(fill)
				if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
					t.Fatalf("width %d fill %d rejected: %v", width, fill, err)
				}
			}
		}
	})

	t.Run("rejects out-of-range line width and fill opacity", func(t *testing.T) {
		enabled := true
		for _, width := range []int{0, MaxDashboardSparklineLineWidth + 1} {
			panel := sparklinePanel("stat", &enabled)
			panel.SparklineLineWidth = new(width)
			if !errors.Is(validateDashboardPanel(panel, map[string]bool{}), ErrInvalidDashboard) {
				t.Fatalf("line width %d accepted", width)
			}
		}
		for _, fill := range []int{-1, MaxDashboardSparklineFillOpacity + 1} {
			panel := sparklinePanel("stat", &enabled)
			panel.SparklineFillOpacity = new(fill)
			if !errors.Is(validateDashboardPanel(panel, map[string]bool{}), ErrInvalidDashboard) {
				t.Fatalf("fill opacity %d accepted", fill)
			}
		}
	})

	t.Run("requires an enabled sparkline", func(t *testing.T) {
		for _, panel := range []DashboardPanel{sparklineOptionsPanel("stat", false), sparklinePanel("stat", nil)} {
			panel.SparklineLineWidth = new(MaxDashboardSparklineLineWidth)
			err := validateDashboardPanel(panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: sparkline line width, fill opacity and gradient mode require an enabled sparkline" {
				t.Fatalf("unexpected error %v", err)
			}
		}
	})

	t.Run("rejects the options on other panel types", func(t *testing.T) {
		for _, panelType := range []string{"timeseries", "text", "row", "table"} {
			if !errors.Is(validateDashboardPanel(sparklineOptionsPanel(panelType, true), map[string]bool{}), ErrInvalidDashboard) {
				t.Fatalf("%s panel accepted sparkline options", panelType)
			}
		}
	})

	t.Run("keeps existing dashboards valid without the options", func(t *testing.T) {
		enabled := true
		if err := validateDashboardPanel(sparklinePanel("stat", &enabled), map[string]bool{}); err != nil {
			t.Fatalf("panel without the options rejected: %v", err)
		}
	})
}

func sparklineGradientPanel(mode string, fill *int) DashboardPanel {
	panel := sparklinePanel("stat", new(true))
	panel.SparklineGradientMode = mode
	panel.SparklineFillOpacity = fill
	return panel
}

func TestValidateDashboardSparklineGradient(t *testing.T) {
	fill := new(20)

	t.Run("accepts every gradient mode once there is a fill", func(t *testing.T) {
		for _, mode := range []string{"", "none", "opacity", "hue"} {
			if err := validateDashboardPanel(sparklineGradientPanel(mode, fill), map[string]bool{}); err != nil {
				t.Fatalf("gradient %q rejected: %v", mode, err)
			}
		}
	})

	t.Run("rejects unknown modes and modes without a fill", func(t *testing.T) {
		for _, mode := range []string{"scheme", "Opacity", "hue "} {
			err := validateDashboardPanel(sparklineGradientPanel(mode, fill), map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: sparkline gradient mode is invalid" {
				t.Fatalf("gradient %q: unexpected error %v", mode, err)
			}
		}
		for _, empty := range []*int{nil, new(0)} {
			err := validateDashboardPanel(sparklineGradientPanel("hue", empty), map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: sparkline gradient mode requires a non-zero fill opacity" {
				t.Fatalf("empty fill: unexpected error %v", err)
			}
		}
	})

	t.Run("requires an enabled sparkline", func(t *testing.T) {
		panel := sparklineGradientPanel("opacity", fill)
		panel.Sparkline = nil
		if !errors.Is(validateDashboardPanel(panel, map[string]bool{}), ErrInvalidDashboard) {
			t.Fatal("gradient accepted without the switch")
		}
	})

	t.Run("rejects the gradient on other panel types", func(t *testing.T) {
		panel := sparklineGradientPanel("hue", fill)
		panel.Type = "timeseries"
		if !errors.Is(validateDashboardPanel(panel, map[string]bool{}), ErrInvalidDashboard) {
			t.Fatal("timeseries panel accepted a sparkline gradient")
		}
	})
}
