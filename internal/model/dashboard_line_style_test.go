package model

import (
	"errors"
	"testing"
)

func intPointer(value int) *int { return &value }

func TestDashboardLineStyleAcceptsDocumentedValues(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	if panel.Type != "timeseries" {
		t.Fatalf("fixture panel must be a timeseries panel, got %q", panel.Type)
	}
	panel.LineWidth = intPointer(MinDashboardLineWidth)
	panel.FillOpacity = intPointer(MaxDashboardFillOpacity)
	panel.ShowPoints = "always"
	panel.Stack = "normal"
	definition.Panels = []DashboardPanel{panel}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("valid line style rejected: %v", err)
	}
	panel.LineWidth = intPointer(MaxDashboardLineWidth)
	panel.FillOpacity = intPointer(0)
	panel.ShowPoints = "never"
	panel.Stack = "none"
	panel.LinePattern = "dotted"
	definition.Panels = []DashboardPanel{panel}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("boundary line style rejected: %v", err)
	}
	for _, pattern := range []string{"", "solid", "dashed", "dotted"} {
		panel.LinePattern = pattern
		definition.Panels = []DashboardPanel{panel}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("line pattern %q rejected: %v", pattern, err)
		}
	}
	for _, connect := range []string{"", "none", "always"} {
		panel.ConnectNulls = connect
		definition.Panels = []DashboardPanel{panel}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("null handling %q rejected: %v", connect, err)
		}
	}
}

func TestDashboardLineStyleRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(*DashboardPanel){
		"width low":       func(panel *DashboardPanel) { panel.LineWidth = intPointer(0) },
		"width high":      func(panel *DashboardPanel) { panel.LineWidth = intPointer(5) },
		"fill low":        func(panel *DashboardPanel) { panel.FillOpacity = intPointer(-1) },
		"fill high":       func(panel *DashboardPanel) { panel.FillOpacity = intPointer(101) },
		"points unknown":  func(panel *DashboardPanel) { panel.ShowPoints = "sometimes" },
		"stack unknown":   func(panel *DashboardPanel) { panel.Stack = "cumulative" },
		"stat line style": func(panel *DashboardPanel) { panel.Type, panel.Stack = "stat", "normal" },
		"pattern unknown": func(panel *DashboardPanel) { panel.LinePattern = "long_dash" },
		"pattern on bar":  func(panel *DashboardPanel) { panel.Type, panel.LinePattern = "barchart", "dashed" },
		"pattern on stat": func(panel *DashboardPanel) { panel.Type, panel.LinePattern = "stat", "dotted" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			change(&definition.Panels[0])
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid line style accepted: %v", err)
			}
		})
	}
}

func TestDashboardGradientAcceptsDocumentedValues(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.FillOpacity = intPointer(30)
	panel.GradientMode = "hue"
	definition.Panels = []DashboardPanel{panel}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("hue gradient rejected: %v", err)
	}
	panel.GradientMode = "opacity"
	definition.Panels = []DashboardPanel{panel}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("opacity gradient rejected: %v", err)
	}
	// 空值与显式 none 等价；柱状图缺省是实心填充，因此不写 fill_opacity 也能开渐变。
	panel.GradientMode = "none"
	definition.Panels = []DashboardPanel{panel}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("explicit none rejected: %v", err)
	}
	bar := definition.Panels[0]
	bar.Type = "barchart"
	bar.LineWidth, bar.FillOpacity, bar.ShowPoints = nil, nil, ""
	bar.GradientMode = "opacity"
	definition.Panels = []DashboardPanel{bar}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("barchart gradient rejected: %v", err)
	}
}

func TestDashboardGradientRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(*DashboardPanel){
		"unknown mode": func(panel *DashboardPanel) { panel.GradientMode = "scheme" },
		"missing fill": func(panel *DashboardPanel) { panel.GradientMode = "hue" },
		"zero fill":    func(panel *DashboardPanel) { panel.FillOpacity, panel.GradientMode = intPointer(0), "opacity" },
		"gauge gradient": func(panel *DashboardPanel) {
			panel.Type, panel.FillOpacity, panel.GradientMode = "stat", intPointer(50), "hue"
		},
		"barchart zero fill": func(panel *DashboardPanel) {
			panel.Type, panel.FillOpacity, panel.GradientMode = "barchart", intPointer(0), "opacity"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			change(&definition.Panels[0])
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid gradient accepted: %v", err)
			}
		})
	}
}

func TestDashboardLineInterpolationAcceptsDocumentedValues(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	for _, interpolation := range []string{"", "linear", "smooth", "step_before", "step_after"} {
		panel := definition.Panels[0]
		panel.LineInterpolation = interpolation
		definition.Panels = []DashboardPanel{panel}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("interpolation %q rejected: %v", interpolation, err)
		}
	}
}

func TestDashboardLineInterpolationRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(*DashboardPanel){
		"unknown mode":       func(panel *DashboardPanel) { panel.LineInterpolation = "cubic" },
		"grafana camel case": func(panel *DashboardPanel) { panel.LineInterpolation = "stepBefore" },
		"barchart":           func(panel *DashboardPanel) { panel.Type, panel.LineInterpolation = "barchart", "smooth" },
		"stat":               func(panel *DashboardPanel) { panel.Type, panel.LineInterpolation = "stat", "smooth" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			change(&definition.Panels[0])
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid interpolation accepted: %v", err)
			}
		})
	}
}
