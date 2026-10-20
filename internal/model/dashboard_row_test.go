package model

import (
	"errors"
	"testing"
)

// rowDefinition 把默认看板的单面板改造成行面板，便于逐项验证行专属校验。
func rowDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type, panel.Size, panel.Legend = "row", "large", false
	panel.Unit, panel.Series = "", nil
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardRowAcceptsGroupingOnlyPayloads(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"expanded":  func(panel *DashboardPanel) {},
		"collapsed": func(panel *DashboardPanel) { panel.RowCollapsed = boolPointer(true) },
		"half width": func(panel *DashboardPanel) {
			panel.Size, panel.RowCollapsed = "medium", boolPointer(false)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(rowDefinition(change)); err != nil {
				t.Fatalf("valid row rejected: %v", err)
			}
		})
	}
}

func TestDashboardRowRejectsPanelOptions(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"series": func(panel *DashboardPanel) {
			panel.Series = []DashboardSeries{{RefID: "A", NodeID: "$node", Metric: "cpu", Aggregation: "avg", Color: "blue"}}
		},
		"legend":       func(panel *DashboardPanel) { panel.Legend = true },
		"unit":         func(panel *DashboardPanel) { panel.Unit = "percent" },
		"thresholds":   func(panel *DashboardPanel) { panel.Thresholds = []DashboardThreshold{{Value: 1, Color: "amber"}} },
		"axes":         func(panel *DashboardPanel) { panel.Min, panel.Max = floatPointer(0), floatPointer(100) },
		"decimals":     func(panel *DashboardPanel) { panel.Decimals = intPointer(2) },
		"repeat":       func(panel *DashboardPanel) { panel.Repeat = "node" },
		"time shift":   func(panel *DashboardPanel) { panel.TimeShift = "1d" },
		"max points":   func(panel *DashboardPanel) { panel.MaxPoints = intPointer(100) },
		"line width":   func(panel *DashboardPanel) { panel.LineWidth = intPointer(3) },
		"fill opacity": func(panel *DashboardPanel) { panel.FillOpacity = intPointer(20) },
		"show points":  func(panel *DashboardPanel) { panel.ShowPoints = "always" },
		"stack":        func(panel *DashboardPanel) { panel.Stack = "normal" },
		"bar display":  func(panel *DashboardPanel) { panel.BarDisplay = "lcd" },
		"pie label":    func(panel *DashboardPanel) { panel.PieLabelMode = "both" },
		"heatmap":      func(panel *DashboardPanel) { panel.HeatmapBuckets = intPointer(8) },
		"histogram":    func(panel *DashboardPanel) { panel.HistogramScale = "log" },
		"timeline":     func(panel *DashboardPanel) { panel.TimelineShowValues = "always" },
		"calculation":  func(panel *DashboardPanel) { panel.Calculation = "max" },
		"legend sort":  func(panel *DashboardPanel) { panel.LegendSort = "asc" },
		"legend values": func(panel *DashboardPanel) {
			panel.LegendValues = []string{"last"}
		},
		"tooltip": func(panel *DashboardPanel) { panel.Tooltip = "single" },
		"value mappings": func(panel *DashboardPanel) {
			panel.ValueMappings = []DashboardValueMapping{{Type: "value", Value: floatPointer(1), Text: "one"}}
		},
		"transformations": func(panel *DashboardPanel) {
			panel.Transformations = []DashboardTransformation{{Kind: "limit", Count: intPointer(2)}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(rowDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("row panel option %s = %v, want invalid dashboard", name, err)
			}
		})
	}
}

func TestDashboardRowCollapseIsRowOnly(t *testing.T) {
	if err := ValidateDashboard(rowDefinition(func(panel *DashboardPanel) { panel.Type = "timeseries" })); err == nil {
		t.Fatal("timeseries panel without series was accepted")
	}
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].RowCollapsed = boolPointer(true)
	if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
		t.Fatalf("timeseries with row_collapsed = %v, want invalid dashboard", err)
	}
}
