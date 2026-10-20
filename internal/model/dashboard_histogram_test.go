package model

import (
	"errors"
	"testing"
)

func histogramDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "histogram"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardHistogramAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults": func(panel *DashboardPanel) {},
		"explicit": func(panel *DashboardPanel) {
			panel.HistogramBuckets, panel.HistogramScale, panel.HistogramColor = intPointer(24), "log", "violet"
			panel.HistogramMerge = boolPointer(true)
		},
		"minimum buckets": func(panel *DashboardPanel) { panel.HistogramBuckets = intPointer(MinDashboardHistogramBuckets) },
		"maximum buckets": func(panel *DashboardPanel) { panel.HistogramBuckets = intPointer(MaxDashboardHistogramBuckets) },
		"linear scale":    func(panel *DashboardPanel) { panel.HistogramScale = "linear" },
		"merge off":       func(panel *DashboardPanel) { panel.HistogramMerge = boolPointer(false) },
		"multi series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(histogramDefinition(change)); err != nil {
				t.Fatalf("valid histogram rejected: %v", err)
			}
		})
	}
}

func TestDashboardHistogramRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"too few buckets":  func(panel *DashboardPanel) { panel.HistogramBuckets = intPointer(MinDashboardHistogramBuckets - 1) },
		"too many buckets": func(panel *DashboardPanel) { panel.HistogramBuckets = intPointer(MaxDashboardHistogramBuckets + 1) },
		"unknown scale":    func(panel *DashboardPanel) { panel.HistogramScale = "sqrt" },
		"unknown color":    func(panel *DashboardPanel) { panel.HistogramColor = "magenta" },
		"value mappings": func(panel *DashboardPanel) {
			panel.ValueMappings = []DashboardValueMapping{{Type: "value", Value: floatPointer(1), Text: "one"}}
		},
		"timeseries buckets":   func(panel *DashboardPanel) { panel.Type, panel.HistogramBuckets = "timeseries", intPointer(12) },
		"stat scale":           func(panel *DashboardPanel) { panel.Type, panel.HistogramScale = "stat", "log" },
		"pie color":            func(panel *DashboardPanel) { panel.Type, panel.HistogramColor = "piechart", "cyan" },
		"heatmap merge":        func(panel *DashboardPanel) { panel.Type, panel.HistogramMerge = "heatmap", boolPointer(true) },
		"table merge":          func(panel *DashboardPanel) { panel.Type, panel.HistogramMerge = "table", boolPointer(false) },
		"barchart buckets":     func(panel *DashboardPanel) { panel.Type, panel.HistogramBuckets = "barchart", intPointer(8) },
		"state timeline merge": func(panel *DashboardPanel) { panel.Type, panel.HistogramMerge = "statetimeline", boolPointer(true) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(histogramDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid histogram accepted: %v", err)
			}
		})
	}
}

func TestDashboardHistogramDefaults(t *testing.T) {
	if got := DefaultDashboardHistogramBuckets(); got != 12 {
		t.Fatalf("default histogram buckets must be 12, got %d", got)
	}
	if got := DefaultDashboardHistogramScale(); got != "linear" {
		t.Fatalf("default histogram scale must be linear, got %q", got)
	}
	if got := DefaultDashboardHistogramColor(); got != "blue" {
		t.Fatalf("default histogram color must be blue, got %q", got)
	}
	if got := DefaultDashboardHistogramMerge(); got {
		t.Fatalf("default histogram merge must be false, got %v", got)
	}
	if MinDashboardHistogramBuckets != 2 || MaxDashboardHistogramBuckets != 64 {
		t.Fatalf("histogram bucket bounds must stay 2..64, got %d..%d", MinDashboardHistogramBuckets, MaxDashboardHistogramBuckets)
	}
}
