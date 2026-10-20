package model

import (
	"errors"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func barGaugeDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "bargauge"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardBarGaugeAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults":      func(panel *DashboardPanel) {},
		"auto display":  func(panel *DashboardPanel) { panel.BarOrientation, panel.BarDisplay = "auto", "basic" },
		"vertical lcd":  func(panel *DashboardPanel) { panel.BarOrientation, panel.BarDisplay = "vertical", "lcd" },
		"horizontal":    func(panel *DashboardPanel) { panel.BarOrientation, panel.BarDisplay = "horizontal", "gradient" },
		"hidden track":  func(panel *DashboardPanel) { panel.BarUnfilled = boolPointer(false) },
		"visible track": func(panel *DashboardPanel) { panel.BarUnfilled = boolPointer(true) },
		"multi series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(barGaugeDefinition(change)); err != nil {
				t.Fatalf("valid bar gauge rejected: %v", err)
			}
		})
	}
}

func TestDashboardBarGaugeRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"unknown orientation": func(panel *DashboardPanel) { panel.BarOrientation = "diagonal" },
		"unknown display":     func(panel *DashboardPanel) { panel.BarDisplay = "neon" },
		"stat orientation":    func(panel *DashboardPanel) { panel.Type, panel.BarOrientation = "stat", "vertical" },
		"gauge display":       func(panel *DashboardPanel) { panel.Type, panel.BarDisplay = "gauge", "lcd" },
		"table track":         func(panel *DashboardPanel) { panel.Type, panel.BarUnfilled = "table", boolPointer(false) },
		"timeseries display":  func(panel *DashboardPanel) { panel.Type, panel.BarDisplay = "timeseries", "basic" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(barGaugeDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid bar gauge accepted: %v", err)
			}
		})
	}
}

func TestDashboardBarOrientationDefaults(t *testing.T) {
	if orientation := DefaultDashboardBarOrientation(1); orientation != "vertical" {
		t.Fatalf("single series must default to vertical, got %q", orientation)
	}
	if orientation := DefaultDashboardBarOrientation(3); orientation != "horizontal" {
		t.Fatalf("multiple series must default to horizontal, got %q", orientation)
	}
}
