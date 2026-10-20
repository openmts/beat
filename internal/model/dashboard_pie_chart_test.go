package model

import (
	"errors"
	"testing"
)

func pieChartDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "piechart"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardPieChartAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults":        func(panel *DashboardPanel) {},
		"explicit pie":    func(panel *DashboardPanel) { panel.PieChartType, panel.PieLabelMode = "pie", "percent" },
		"donut":           func(panel *DashboardPanel) { panel.PieChartType = "donut" },
		"value labels":    func(panel *DashboardPanel) { panel.PieLabelMode = "value" },
		"combined labels": func(panel *DashboardPanel) { panel.PieLabelMode = "both" },
		"multi series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(pieChartDefinition(change)); err != nil {
				t.Fatalf("valid pie chart rejected: %v", err)
			}
		})
	}
}

func TestDashboardPieChartRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"unknown type":      func(panel *DashboardPanel) { panel.PieChartType = "radar" },
		"unknown labels":    func(panel *DashboardPanel) { panel.PieLabelMode = "legend" },
		"stat type":         func(panel *DashboardPanel) { panel.Type, panel.PieChartType = "stat", "donut" },
		"gauge labels":      func(panel *DashboardPanel) { panel.Type, panel.PieLabelMode = "gauge", "both" },
		"bar gauge type":    func(panel *DashboardPanel) { panel.Type, panel.PieChartType = "bargauge", "pie" },
		"timeseries labels": func(panel *DashboardPanel) { panel.Type, panel.PieLabelMode = "timeseries", "percent" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(pieChartDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid pie chart accepted: %v", err)
			}
		})
	}
}

func TestDashboardPieChartDefaults(t *testing.T) {
	if got := DefaultDashboardPieChartType(); got != "pie" {
		t.Fatalf("default pie chart type must be pie, got %q", got)
	}
	if got := DefaultDashboardPieLabelMode(); got != "percent" {
		t.Fatalf("default pie label mode must be percent, got %q", got)
	}
}
