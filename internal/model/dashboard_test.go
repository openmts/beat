package model

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestDefaultMetricDashboardsAreValidAndIndependent(t *testing.T) {
	for _, scope := range []string{"public", "admin"} {
		dashboard := DefaultMetricDashboard(scope)
		if err := ValidateDashboardPolicy(dashboard.Definition, dashboardTestPolicy()); err != nil {
			t.Fatal(err)
		}
		if dashboard.Scope != scope || dashboard.Revision != 0 || dashboard.HasPrevious {
			t.Fatal("built-in dashboard claims saved metadata")
		}
		for _, panel := range dashboard.Definition.Panels {
			for _, series := range panel.Series {
				if series.NodeID != "$node" {
					t.Fatal("public default exposes a fixed node")
				}
			}
		}
	}
	copy := DefaultMetricDashboard("public")
	copy.Definition.Panels[0].Series[0].Metric = "changed"
	if DefaultMetricDashboard("public").Definition.Panels[0].Series[0].Metric != "cpu" {
		t.Fatal("dashboard defaults share mutable series")
	}
}

func TestMetricDashboardRejectsInvalidStructure(t *testing.T) {
	cases := map[string]func(*DashboardDefinition){
		"version":            func(d *DashboardDefinition) { d.Version = 999 },
		"title":              func(d *DashboardDefinition) { d.Title = "\x00" },
		"description":        func(d *DashboardDefinition) { d.Description = strings.Repeat("x", 2049) },
		"short range":        func(d *DashboardDefinition) { d.TimeRangeSeconds = 0 },
		"long range":         func(d *DashboardDefinition) { d.TimeRangeSeconds = MaxDashboardRangeSeconds + 1 },
		"refresh":            func(d *DashboardDefinition) { d.RefreshSeconds = 1 },
		"variables":          func(d *DashboardDefinition) { d.Variables = make([]DashboardVariable, 5) },
		"variable duplicate": func(d *DashboardDefinition) { d.Variables = append(d.Variables, d.Variables[0]) },
		"variable name":      func(d *DashboardDefinition) { d.Variables[0].Name = "__proto__" },
		"variable type":      func(d *DashboardDefinition) { d.Variables[0].Type = "script" },
		"variable label":     func(d *DashboardDefinition) { d.Variables[0].Label = "" },
		"variable all":       func(d *DashboardDefinition) { d.Variables[0].IncludeAll = true },
		"variable regex":     func(d *DashboardDefinition) { d.Variables[0].Regex = "(?=a)" },
		"variable regex size": func(d *DashboardDefinition) {
			d.Variables[0].Regex = strings.Repeat("a", MaxDashboardVariableRegexBytes+1)
		},
		"panels":             func(d *DashboardDefinition) { d.Panels = make([]DashboardPanel, 25) },
		"panel duplicate":    func(d *DashboardDefinition) { d.Panels = append(d.Panels, d.Panels[0]) },
		"panel id":           func(d *DashboardDefinition) { d.Panels[0].ID = "bad/id" },
		"panel title":        func(d *DashboardDefinition) { d.Panels[0].Title = "" },
		"panel type":         func(d *DashboardDefinition) { d.Panels[0].Type = "html" },
		"panel size":         func(d *DashboardDefinition) { d.Panels[0].Size = "giant" },
		"series count":       func(d *DashboardDefinition) { d.Panels[0].Series = nil },
		"series excess":      func(d *DashboardDefinition) { d.Panels[0].Series = make([]DashboardSeries, 9) },
		"series duplicate":   func(d *DashboardDefinition) { d.Panels[0].Series = append(d.Panels[0].Series, d.Panels[0].Series[0]) },
		"series id":          func(d *DashboardDefinition) { d.Panels[0].Series[0].RefID = "" },
		"series metric":      func(d *DashboardDefinition) { d.Panels[0].Series[0].Metric = "SELECT value" },
		"series aggregation": func(d *DashboardDefinition) { d.Panels[0].Series[0].Aggregation = "median" },
		"series color":       func(d *DashboardDefinition) { d.Panels[0].Series[0].Color = "url(https://tracker)" },
		"series label":       func(d *DashboardDefinition) { d.Panels[0].Series[0].Label = "\r\n" },
		"series variable":    func(d *DashboardDefinition) { d.Panels[0].Series[0].NodeID = "$unknown" },
		"series node":        func(d *DashboardDefinition) { d.Panels[0].Series[0].NodeID = "../node" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			change(&definition)
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid dashboard accepted: %v", err)
			}
		})
	}
}

func TestDashboardVariableMultiAndRegexAreAccepted(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Variables[0].Multi = true
	definition.Variables[0].IncludeAll = true
	definition.Variables[0].Regex = `^hk-[0-9]+$`
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("valid multi-select variable rejected: %v", err)
	}
}

func TestDashboardPanelRepeatMustReferenceVariable(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Repeat = "node"
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("repeat referencing a declared variable must validate: %v", err)
	}
	definition.Panels[0].Repeat = "missing"
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("repeat referencing an undefined variable must be rejected")
	}
	definition.Panels[0].Repeat = "bad name"
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("repeat with an invalid variable name must be rejected")
	}
}

func TestDashboardAxesAndThresholds(t *testing.T) {
	for name, panel := range dashboardInvalidAxes() {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			definition.Panels = []DashboardPanel{panel}
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid axes accepted: %v", err)
			}
		})
	}
	definition := DefaultMetricDashboard("public").Definition
	definition.Panels[0].Min, definition.Panels[0].Max = dashboardFloat(0), dashboardFloat(100)
	definition.Panels[0].Thresholds = []DashboardThreshold{{Value: 80, Color: "amber"}, {Value: 90, Color: "rose"}}
	definition.Panels[0].Decimals = dashboardInteger(2)
	if err := ValidateDashboard(definition); err != nil {
		t.Fatal(err)
	}
}

func dashboardInvalidAxes() map[string]DashboardPanel {
	result := make(map[string]DashboardPanel)
	for _, name := range []string{"decimals", "infinite min", "nan max", "reversed", "threshold limit", "threshold color", "threshold order", "threshold finite"} {
		panel := DefaultMetricDashboard("public").Definition.Panels[0]
		switch name {
		case "decimals":
			panel.Decimals = dashboardInteger(7)
		case "infinite min":
			panel.Min = dashboardFloat(math.Inf(1))
		case "nan max":
			panel.Max = dashboardFloat(math.NaN())
		case "reversed":
			panel.Min, panel.Max = dashboardFloat(100), dashboardFloat(0)
		case "threshold limit":
			panel.Thresholds = make([]DashboardThreshold, 5)
		case "threshold color":
			panel.Thresholds = []DashboardThreshold{{Value: 1, Color: "arbitrary"}}
		case "threshold order":
			panel.Thresholds = []DashboardThreshold{{Value: 1, Color: "blue"}, {Value: 0, Color: "blue"}}
		case "threshold finite":
			panel.Thresholds = []DashboardThreshold{{Value: math.Inf(-1), Color: "blue"}}
		}
		result[name] = panel
	}
	return result
}

func dashboardFloat(value float64) *float64 { return &value }

func dashboardInteger(value int) *int { return &value }

func dashboardTestPolicy() DashboardPolicy {
	policy := DashboardPolicy{Public: true, Nodes: map[string]bool{"node-one": true}, Metrics: make(map[string]DashboardMetricPolicy)}
	for _, name := range []string{"cpu", "memory", "disk", "net_recv", "net_sent"} {
		unit := "percent"
		if strings.HasPrefix(name, "net_") {
			unit = "bytes_per_second"
		}
		policy.Metrics[name] = DashboardMetricPolicy{Unit: unit, Public: true, Enabled: true, Aggregations: []string{"avg", "last"}}
	}
	return policy
}

func TestDashboardStatNeedsSingleSeries(t *testing.T) {
	definition := DefaultMetricDashboard("public").Definition
	definition.Panels[0].Type = "stat"
	definition.Panels[0].Series = append(definition.Panels[0].Series, definition.Panels[0].Series[0])
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("ambiguous multi-series stat accepted")
	}
}

func TestDashboardLegendOptions(t *testing.T) {
	for name, panel := range dashboardInvalidLegends() {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			definition.Panels = []DashboardPanel{panel}
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid legend accepted: %v", err)
			}
		})
	}
	definition := DefaultMetricDashboard("public").Definition
	definition.Panels[0].Legend = true
	definition.Panels[0].LegendValues = []string{"last", "mean"}
	definition.Panels[0].LegendSort = "desc"
	definition.Panels[0].Tooltip = "single"
	if err := ValidateDashboard(definition); err != nil {
		t.Fatal(err)
	}
	// 隐藏图例时省略取值与排序，等价于显式默认值。
	definition.Panels[0].Legend = false
	definition.Panels[0].LegendValues = nil
	definition.Panels[0].LegendSort = ""
	definition.Panels[0].Tooltip = ""
	if err := ValidateDashboard(definition); err != nil {
		t.Fatal(err)
	}
}

func dashboardInvalidLegends() map[string]DashboardPanel {
	result := make(map[string]DashboardPanel)
	for _, name := range []string{"value limit", "value unknown", "value duplicate", "sort unknown", "tooltip unknown", "hidden with values", "hidden with sort"} {
		panel := DefaultMetricDashboard("public").Definition.Panels[0]
		panel.Legend = true
		switch name {
		case "value limit":
			panel.LegendValues = []string{"last", "min", "max", "mean", "last"}
		case "value unknown":
			panel.LegendValues = []string{"median"}
		case "value duplicate":
			panel.LegendValues = []string{"last", "last"}
		case "sort unknown":
			panel.LegendSort = "ascending"
		case "tooltip unknown":
			panel.Tooltip = "always"
		case "hidden with values":
			panel.Legend = false
			panel.LegendValues = []string{"last"}
		case "hidden with sort":
			panel.Legend = false
			panel.LegendSort = "asc"
		}
		result[name] = panel
	}
	return result
}
