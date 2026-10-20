package model

import (
	"errors"
	"testing"
)

func heatmapDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "heatmap"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardHeatmapAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults": func(panel *DashboardPanel) {},
		"explicit": func(panel *DashboardPanel) {
			panel.HeatmapBuckets, panel.HeatmapScale, panel.HeatmapColor = intPointer(24), "log", "violet"
		},
		"minimum":      func(panel *DashboardPanel) { panel.HeatmapBuckets = intPointer(MinDashboardHeatmapBuckets) },
		"maximum":      func(panel *DashboardPanel) { panel.HeatmapBuckets = intPointer(MaxDashboardHeatmapBuckets) },
		"linear scale": func(panel *DashboardPanel) { panel.HeatmapScale = "linear" },
		"multi series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
		"transformations": func(panel *DashboardPanel) {
			panel.Transformations = []DashboardTransformation{{Kind: "limit", Count: intPointer(3)}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(heatmapDefinition(change)); err != nil {
				t.Fatalf("valid heatmap rejected: %v", err)
			}
		})
	}
}

func TestDashboardHeatmapRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"too few buckets":  func(panel *DashboardPanel) { panel.HeatmapBuckets = intPointer(MinDashboardHeatmapBuckets - 1) },
		"too many buckets": func(panel *DashboardPanel) { panel.HeatmapBuckets = intPointer(MaxDashboardHeatmapBuckets + 1) },
		"unknown scale":    func(panel *DashboardPanel) { panel.HeatmapScale = "sqrt" },
		"unknown color":    func(panel *DashboardPanel) { panel.HeatmapColor = "magenta" },
		"value mappings": func(panel *DashboardPanel) {
			panel.ValueMappings = []DashboardValueMapping{{Type: "value", Value: floatPointer(1), Text: "one"}}
		},
		"calculation":        func(panel *DashboardPanel) { panel.Calculation = "avg" },
		"timeseries buckets": func(panel *DashboardPanel) { panel.Type, panel.HeatmapBuckets = "timeseries", intPointer(12) },
		"stat scale":         func(panel *DashboardPanel) { panel.Type, panel.HeatmapScale = "stat", "log" },
		"pie color":          func(panel *DashboardPanel) { panel.Type, panel.HeatmapColor = "piechart", "cyan" },
		"table buckets":      func(panel *DashboardPanel) { panel.Type, panel.HeatmapBuckets = "table", intPointer(8) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(heatmapDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid heatmap accepted: %v", err)
			}
		})
	}
}

func TestDashboardHeatmapDefaults(t *testing.T) {
	if got := DefaultDashboardHeatmapBuckets(); got != 12 {
		t.Fatalf("default heatmap buckets must be 12, got %d", got)
	}
	if got := DefaultDashboardHeatmapScale(); got != "linear" {
		t.Fatalf("default heatmap scale must be linear, got %q", got)
	}
	if got := DefaultDashboardHeatmapColor(); got != "blue" {
		t.Fatalf("default heatmap color must be blue, got %q", got)
	}
	if MinDashboardHeatmapBuckets != 2 || MaxDashboardHeatmapBuckets != 64 {
		t.Fatalf("heatmap bucket bounds must stay 2..64, got %d..%d", MinDashboardHeatmapBuckets, MaxDashboardHeatmapBuckets)
	}
}
