package model

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func overrideBool(value bool) *bool { return &value }

func overrideDefinition(overrides []DashboardOverride) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Overrides = overrides
	return definition
}

func TestDashboardOverridesAcceptDocumentedShapes(t *testing.T) {
	overrides := []DashboardOverride{
		{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, Color: "rose"},
		{Matcher: DashboardOverrideMatcher{Type: "by_regex", Value: "^node-.*$"}, Unit: "percent", HideLegend: overrideBool(true)},
		{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "Memory usage"}, Color: "violet", Unit: "bytes", HideLegend: overrideBool(false)},
	}
	for _, kind := range []string{"timeseries", "stat", "gauge", "table", "barchart", "bargauge", "piechart", "heatmap"} {
		definition := overrideDefinition(overrides)
		definition.Panels[0].Type = kind
		if kind == "stat" || kind == "gauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected valid overrides: %v", kind, err)
		}
	}
}

func TestDashboardOverridesDefaultToIdentity(t *testing.T) {
	for _, overrides := range [][]DashboardOverride{nil, {}} {
		if err := ValidateDashboard(overrideDefinition(overrides)); err != nil {
			t.Fatalf("absent overrides must stay valid: %v", err)
		}
	}
}

func TestDashboardOverridesRejectInvalidShapes(t *testing.T) {
	cases := map[string]struct {
		kind      string
		overrides []DashboardOverride
	}{
		"too many":                 {overrides: make([]DashboardOverride, MaxDashboardOverrides+1)},
		"empty list":               {overrides: []DashboardOverride{{}}},
		"matcher type":             {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_label", Value: "a"}, Color: "rose"}}},
		"empty value":              {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name"}, Color: "rose"}}},
		"blank value":              {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "  "}, Color: "rose"}}},
		"long value":               {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: strings.Repeat("a", MaxTransformationPatternBytes+1)}, Color: "rose"}}},
		"control char":             {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a\u0000"}, Color: "rose"}}},
		"bad regex":                {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_regex", Value: "(["}, Color: "rose"}}},
		"color":                    {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, Color: "chartreuse"}}},
		"unit bytes":               {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, Unit: strings.Repeat("u", MaxDashboardOverrideUnitBytes+1)}}},
		"unit control":             {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, Unit: "percent\u0000"}}},
		"no property":              {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}}}},
		"line width low":           {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, LineWidth: intPointer(MinDashboardLineWidth - 1)}}},
		"line width high":          {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, LineWidth: intPointer(MaxDashboardLineWidth + 1)}}},
		"fill opacity low":         {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, FillOpacity: intPointer(-1)}}},
		"fill opacity high":        {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, FillOpacity: intPointer(MaxDashboardFillOpacity + 1)}}},
		"line width on barchart":   {kind: "barchart", overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, LineWidth: intPointer(DefaultDashboardLineWidth)}}},
		"fill opacity on piechart": {kind: "piechart", overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, FillOpacity: intPointer(50)}}},
		"axis placement value":     {overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, AxisPlacement: "centre"}}},
		"axis placement on table":  {kind: "table", overrides: []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "a"}, AxisPlacement: DashboardAxisPlacementRight}}},
	}
	for name, testCase := range cases {
		definition := overrideDefinition(testCase.overrides)
		if testCase.kind != "" {
			definition.Panels[0].Type = testCase.kind
		}
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s: expected ErrInvalidDashboard, got %v", name, err)
		}
	}
}

// TestDashboardOverridesAcceptSeriesLineStyle 覆盖两个新属性的合法组合：折线可同时改线宽与填充，
// 柱状图只改填充；最小值与上限都必须被接受。
func TestDashboardOverridesAcceptSeriesLineStyle(t *testing.T) {
	width, opacity := MinDashboardLineWidth, MaxDashboardFillOpacity
	matcher := DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}
	cases := map[string]struct {
		kind      string
		overrides []DashboardOverride
	}{
		"timeseries":         {kind: "timeseries", overrides: []DashboardOverride{{Matcher: matcher, LineWidth: &width, FillOpacity: &opacity}}},
		"barchart zero fill": {kind: "barchart", overrides: []DashboardOverride{{Matcher: matcher, FillOpacity: intPointer(0)}}},
		"barchart full fill": {kind: "barchart", overrides: []DashboardOverride{{Matcher: matcher, FillOpacity: intPointer(MaxDashboardFillOpacity)}}},
	}
	for name, testCase := range cases {
		definition := overrideDefinition(testCase.overrides)
		definition.Panels[0].Type = testCase.kind
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected valid series line style overrides: %v", name, err)
		}
	}
}

// TestDashboardOverridesAcceptSeriesAxisPlacement 按序列轴位置只属于折线/柱状面板：左右两个取值都必须
// 被接受，且它与其它属性可以叠加在同一条覆盖上。
func TestDashboardOverridesAcceptSeriesAxisPlacement(t *testing.T) {
	matcher := DashboardOverrideMatcher{Type: "by_name", Value: "Memory usage"}
	cases := map[string]struct {
		kind      string
		overrides []DashboardOverride
	}{
		"timeseries right": {kind: "timeseries", overrides: []DashboardOverride{{Matcher: matcher, AxisPlacement: DashboardAxisPlacementRight, Color: "violet"}}},
		"timeseries left":  {kind: "timeseries", overrides: []DashboardOverride{{Matcher: matcher, AxisPlacement: DashboardAxisPlacementLeft}}},
		"barchart right":   {kind: "barchart", overrides: []DashboardOverride{{Matcher: matcher, AxisPlacement: DashboardAxisPlacementRight}}},
	}
	for name, testCase := range cases {
		definition := overrideDefinition(testCase.overrides)
		definition.Panels[0].Type = testCase.kind
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected a valid series axis placement override: %v", name, err)
		}
	}
}

func TestDashboardOverridesAreRejectedOnPanelsWithoutSeries(t *testing.T) {
	for _, kind := range []string{"row", "text"} {
		definition := DefaultMetricDashboard("admin").Definition
		panel := DashboardPanel{ID: "overridden", Title: "Overridden", Type: kind, Size: "medium", Overrides: []DashboardOverride{
			{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, Color: "rose"},
		}}
		if kind == "text" {
			panel.Text = "hello"
		}
		definition.Panels = append(definition.Panels, panel)
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s panel must reject overrides, got %v", kind, err)
		}
	}
}

func TestDashboardOverridesAcceptQueryRefIDMatcher(t *testing.T) {
	overrides := []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: "B"}, Color: "amber"}}
	for _, kind := range []string{"timeseries", "barchart", "stat", "piechart"} {
		definition := overrideDefinition(overrides)
		definition.Panels[0].Type = kind
		if kind == "stat" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected a by_ref_id override: %v", kind, err)
		}
	}
	invalid := []DashboardOverride{
		{Matcher: DashboardOverrideMatcher{Type: "byQuery", Value: "A"}, Color: "amber"},
		{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: ""}, Color: "amber"},
		{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: string(make([]byte, MaxTransformationPatternBytes+1))}, Color: "amber"},
	}
	for _, override := range invalid {
		if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("invalid ref_id matcher accepted: %v", err)
		}
	}
}

func TestDashboardOverridesAcceptPerSeriesThresholds(t *testing.T) {
	thresholds := []DashboardThreshold{{Value: 70, Color: "amber"}, {Value: 90, Color: "rose"}}
	overrides := []DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: "A"}, Thresholds: thresholds}}
	for _, kind := range []string{"stat", "gauge", "bargauge", "table", "statetimeline"} {
		definition := overrideDefinition(overrides)
		definition.Panels[0].Type = kind
		if kind == "stat" || kind == "gauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected per-series thresholds: %v", kind, err)
		}
	}
	// 折线/柱状不按单值阈值取色，饼图只按序列色渲染：带上阈值只会保存出渲染器忽略的配置。
	for _, kind := range []string{"timeseries", "barchart", "piechart"} {
		definition := overrideDefinition(overrides)
		definition.Panels[0].Type = kind
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s panel accepted per-series thresholds: %v", kind, err)
		}
	}
}

func TestDashboardOverridesAcceptFillBelowTo(t *testing.T) {
	override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, FillBelowTo: "Memory usage"}
	if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); err != nil {
		t.Fatalf("time series panel rejected fill below to: %v", err)
	}
	// 堆叠按层叠面积绘制，和「填充到另一条线」互斥；其它面板类型根本没有折线可填。
	stacked := overrideDefinition([]DashboardOverride{override})
	stacked.Panels[0].Stack = "normal"
	if err := ValidateDashboard(stacked); !errors.Is(err, ErrInvalidDashboard) {
		t.Fatalf("stacked panel accepted fill below to: %v", err)
	}
	for _, kind := range []string{"barchart", "stat", "gauge", "bargauge", "table", "piechart", "heatmap", "statetimeline", "geomap"} {
		definition := overrideDefinition([]DashboardOverride{override})
		definition.Panels[0].Type = kind
		if kind == "stat" || kind == "gauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s panel accepted fill below to: %v", kind, err)
		}
	}
}

func TestDashboardOverridesRejectUnusableFillBelowTo(t *testing.T) {
	for name, value := range map[string]string{
		"blank":       "   ",
		"control":     "Memory\u0001usage",
		"too long":    strings.Repeat("a", MaxDashboardOverrideFillBelowBytes+1),
		"invalid utf": string([]byte{0xff, 0xfe}),
	} {
		override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, FillBelowTo: value}
		if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s: invalid fill below to accepted: %v", name, err)
		}
	}
}

func TestDashboardOverridesCountFillBelowToAsProperty(t *testing.T) {
	// 只带 fill_below_to 的覆盖是真实属性，不能被当成空规则拒绝。
	override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: "A"}, FillBelowTo: "Memory usage"}
	if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); err != nil {
		t.Fatalf("fill below to alone must count as a property: %v", err)
	}
}

// 逐序列轴选项（Grafana 的 Overrides → Axis）：标题与柔性上下限只在折线/柱状面板上生效。
func TestDashboardOverridesAcceptAxisOptions(t *testing.T) {
	override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisLabel: "CPU %",
		AxisSoftMin: floatPointer(0), AxisSoftMax: floatPointer(100)}
	for _, kind := range []string{"timeseries", "barchart"} {
		definition := overrideDefinition([]DashboardOverride{override})
		definition.Panels[0].Type = kind
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected axis options: %v", kind, err)
		}
	}
}

func TestDashboardOverridesRejectUnusableAxisOptions(t *testing.T) {
	cases := map[string]DashboardOverride{
		"label too long": {Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisLabel: strings.Repeat("a", MaxDashboardAxisLabelBytes+1)},
		"label control":  {Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisLabel: "CPU\u0000"},
		"soft order":     {Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisSoftMin: floatPointer(90), AxisSoftMax: floatPointer(10)},
		"soft infinite":  {Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisSoftMin: floatPointer(math.Inf(1))},
	}
	for name, override := range cases {
		if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s: unusable axis option accepted: %v", name, err)
		}
	}
	// 面板级固定上下限会让柔性值永远不生效，因此这种组合直接拒绝。
	fixed := overrideDefinition([]DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisSoftMin: floatPointer(0)}})
	fixed.Panels[0].Min = floatPointer(0)
	if err := ValidateDashboard(fixed); !errors.Is(err, ErrInvalidDashboard) {
		t.Fatalf("soft axis limit with a fixed panel limit accepted: %v", err)
	}
	// 其它面板类型根本没有值轴。
	stat := overrideDefinition([]DashboardOverride{{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, AxisLabel: "CPU"}})
	stat.Panels[0].Type, stat.Panels[0].Series = "stat", stat.Panels[0].Series[:1]
	if err := ValidateDashboard(stat); !errors.Is(err, ErrInvalidDashboard) {
		t.Fatalf("stat panel accepted axis options: %v", err)
	}
}

func TestDashboardOverridesCountAxisOptionsAsProperty(t *testing.T) {
	// 只带 axis_label 的覆盖是真实属性，不能被当成空规则拒绝。
	override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_ref_id", Value: "A"}, AxisLabel: "CPU"}
	if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); err != nil {
		t.Fatalf("axis label alone must count as a property: %v", err)
	}
}

func TestDashboardOverridesRejectMalformedPerSeriesThresholds(t *testing.T) {
	cases := map[string][]DashboardThreshold{
		"empty color":      {{Value: 70}},
		"unsorted":         {{Value: 90, Color: "rose"}, {Value: 70, Color: "amber"}},
		"duplicate values": {{Value: 70, Color: "amber"}, {Value: 70, Color: "rose"}},
		"too many":         {{Value: 1, Color: "amber"}, {Value: 2, Color: "rose"}, {Value: 3, Color: "blue"}, {Value: 4, Color: "green"}, {Value: 5, Color: "violet"}},
	}
	for name, thresholds := range cases {
		override := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}, Thresholds: thresholds}
		definition := overrideDefinition([]DashboardOverride{override})
		definition.Panels[0].Type = "stat"
		definition.Panels[0].Series = definition.Panels[0].Series[:1]
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s: invalid per-series thresholds accepted: %v", name, err)
		}
	}
}

func TestDashboardOverridesAcceptFieldTypeAndValueMatchers(t *testing.T) {
	overrides := []DashboardOverride{
		{Matcher: DashboardOverrideMatcher{Type: "by_type", Value: "percent"}, Color: "rose"},
		{Matcher: DashboardOverrideMatcher{Type: "by_value", Value: "0"}, HideLegend: overrideBool(true)},
		{Matcher: DashboardOverrideMatcher{Type: "by_value", Value: "-1.5"}, Unit: "bytes"},
	}
	for _, kind := range []string{"timeseries", "barchart", "stat", "piechart"} {
		definition := overrideDefinition(overrides)
		definition.Panels[0].Type = kind
		if kind == "stat" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected the field type/value matchers: %v", kind, err)
		}
	}
	// 类型必须落在枚举里，值必须能解析成有限数：两种都不可能命中的规则直接拒绝。
	invalid := []DashboardOverride{
		{Matcher: DashboardOverrideMatcher{Type: "by_type", Value: "gigabytes"}, Color: "rose"},
		{Matcher: DashboardOverrideMatcher{Type: "by_type", Value: ""}, Color: "rose"},
		{Matcher: DashboardOverrideMatcher{Type: "by_value", Value: "zero"}, Color: "rose"},
		{Matcher: DashboardOverrideMatcher{Type: "by_value", Value: "NaN"}, Color: "rose"},
	}
	for _, override := range invalid {
		if err := ValidateDashboard(overrideDefinition([]DashboardOverride{override})); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("invalid type/value matcher accepted: %v", err)
		}
	}
}

func TestDashboardOverridesAcceptStandardOptions(t *testing.T) {
	standard := DashboardOverride{Matcher: DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"},
		DisplayName: "CPU", Decimals: dashboardInteger(1), Min: dashboardFloat(0), Max: dashboardFloat(90)}
	for _, kind := range []string{"stat", "gauge", "bargauge"} {
		definition := overrideDefinition([]DashboardOverride{standard})
		definition.Panels[0].Type = kind
		if kind != "bargauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected the standard options override: %v", kind, err)
		}
	}
	// 显示名与小数位跟着「会给序列渲染名字与数值」的面板，值域只在有单值量程的面板上生效。
	labelled := standard
	labelled.Min, labelled.Max = nil, nil
	for _, kind := range []string{"timeseries", "barchart", "stat", "gauge", "bargauge", "table", "statetimeline", "piechart"} {
		definition := overrideDefinition([]DashboardOverride{labelled})
		definition.Panels[0].Type = kind
		definition.Panels[0].Series = definition.Panels[0].Series[:1]
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected the labelled override: %v", kind, err)
		}
	}
	for _, kind := range []string{"table", "statetimeline", "piechart", "heatmap", "timeseries"} {
		definition := overrideDefinition([]DashboardOverride{standard})
		definition.Panels[0].Type = kind
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s panel accepted override value limits: %v", kind, err)
		}
	}
}

func TestDashboardOverridesRejectUnusableStandardOptions(t *testing.T) {
	matcher := DashboardOverrideMatcher{Type: "by_name", Value: "CPU usage"}
	cases := []struct {
		override DashboardOverride
		kind     string
	}{
		{DashboardOverride{Matcher: matcher, Decimals: dashboardInteger(7)}, "stat"},
		{DashboardOverride{Matcher: matcher, Decimals: dashboardInteger(-1)}, "stat"},
		{DashboardOverride{Matcher: matcher, DisplayName: strings.Repeat("x", MaxDashboardOverrideDisplayNameBytes+1)}, "stat"},
		{DashboardOverride{Matcher: matcher, DisplayName: "bad\nname"}, "stat"},
		{DashboardOverride{Matcher: matcher, Min: dashboardFloat(math.Inf(1))}, "stat"},
		{DashboardOverride{Matcher: matcher, Min: dashboardFloat(90), Max: dashboardFloat(90)}, "stat"},
		{DashboardOverride{Matcher: matcher, Max: dashboardFloat(90)}, "heatmap"},
		{DashboardOverride{Matcher: matcher, DisplayName: "CPU"}, "text"},
	}
	for index, item := range cases {
		definition := overrideDefinition([]DashboardOverride{item.override})
		definition.Panels[0].Type = item.kind
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("case %d accepted an unusable standard options override: %v", index, err)
		}
	}
}
