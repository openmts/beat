package model

import (
	"errors"
	"testing"
)

func stateTimelineDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "statetimeline"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardStateTimelineAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults": func(panel *DashboardPanel) {},
		"explicit": func(panel *DashboardPanel) {
			panel.TimelineMerge, panel.TimelineShowValues = boolPointer(false), "always"
		},
		"merge on":   func(panel *DashboardPanel) { panel.TimelineMerge = boolPointer(true) },
		"labels off": func(panel *DashboardPanel) { panel.TimelineShowValues = "never" },
		"multi series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
		"value mappings": func(panel *DashboardPanel) {
			panel.ValueMappings = []DashboardValueMapping{{Type: "value", Value: floatPointer(1), Text: "Up", Color: "green"}}
		},
		"thresholds": func(panel *DashboardPanel) {
			panel.Thresholds = []DashboardThreshold{{Value: 50, Color: "amber"}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(stateTimelineDefinition(change)); err != nil {
				t.Fatalf("valid state timeline rejected: %v", err)
			}
		})
	}
}

func TestDashboardStateTimelineRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"unknown label mode": func(panel *DashboardPanel) { panel.TimelineShowValues = "sometimes" },
		"timeseries merge":   func(panel *DashboardPanel) { panel.Type, panel.TimelineMerge = "timeseries", boolPointer(false) },
		"heatmap labels":     func(panel *DashboardPanel) { panel.Type, panel.TimelineShowValues = "heatmap", "always" },
		"stat merge":         func(panel *DashboardPanel) { panel.Type, panel.TimelineMerge = "stat", boolPointer(true) },
		"gauge labels":       func(panel *DashboardPanel) { panel.Type, panel.TimelineShowValues = "gauge", "auto" },
		"table merge":        func(panel *DashboardPanel) { panel.Type, panel.TimelineMerge = "table", boolPointer(true) },
		"barchart labels":    func(panel *DashboardPanel) { panel.Type, panel.TimelineShowValues = "barchart", "never" },
		"calculation":        func(panel *DashboardPanel) { panel.Calculation = "avg" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(stateTimelineDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid state timeline accepted: %v", err)
			}
		})
	}
}

func TestDashboardStateTimelineDefaults(t *testing.T) {
	if got := DefaultDashboardStateTimelineMerge(); !got {
		t.Fatal("state timeline must merge equal neighbours by default")
	}
	if got := DefaultDashboardStateTimelineShowValues(); got != "auto" {
		t.Fatalf("default value label mode must be auto, got %q", got)
	}
}
