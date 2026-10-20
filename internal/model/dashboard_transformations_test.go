package model

import (
	"errors"
	"strings"
	"testing"
)

func transformationDefinition(transformations []DashboardTransformation) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Transformations = transformations
	return definition
}

func TestDashboardTransformationsAcceptDocumentedShapes(t *testing.T) {
	chain := []DashboardTransformation{
		{Kind: "rename_by_regex", Pattern: "^cpu$", Replacement: "CPU"},
		{Kind: "filter_by_value", Comparison: "gt", Value: floatPointer(0)},
		{Kind: "sort_by", Statistic: "mean", Order: "desc"},
		{Kind: "limit", Count: intPointer(3)},
	}
	for _, kind := range []string{"timeseries", "stat", "gauge", "table", "bargauge", "piechart"} {
		definition := transformationDefinition(chain)
		definition.Panels[0].Type = kind
		if kind == "stat" || kind == "gauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected a valid transformation chain: %v", kind, err)
		}
	}
}

// 二元运算同样单列一条链：左/右选择器支持字面标签与 /re/ 正则两种写法。
func TestDashboardBinaryOperationTransformationAcceptsSelectors(t *testing.T) {
	for _, transformation := range []DashboardTransformation{
		{Kind: "binary_operation", Left: "cpu.user", Operator: "+", Right: "cpu.idle"},
		{Kind: "binary_operation", Left: "/node-.*/", Operator: "/", Right: "cpu.total", Alias: "ratio"},
		{Kind: "binary_operation", Left: "/a\\d+/", Operator: "*", Right: "/b\\d+/"},
	} {
		if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
			t.Fatalf("binary operation %+v rejected: %v", transformation, err)
		}
	}
}

// reduce 的取值集合与面板级 Calculation 共用一份，因此这里逐个取值确认都被接受。
func TestDashboardReduceTransformationAcceptsEveryCalculation(t *testing.T) {
	for _, calculation := range dashboardCalculations {
		transformation := DashboardTransformation{Kind: "reduce", Calculation: calculation}
		if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
			t.Fatalf("reduce %s rejected: %v", calculation, err)
		}
	}
}

// Reduce 的 series_to_rows 模式允许一次选多个计算方式；reduce_fields 保持单值口径。
func TestDashboardReduceTransformationAcceptsSeriesToRowsCalculations(t *testing.T) {
	transformation := DashboardTransformation{Kind: "reduce", Mode: "series_to_rows", Calculations: []string{"min", "max", "mean"}}
	if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
		t.Fatalf("series to rows reject: %v", err)
	}
	single := DashboardTransformation{Kind: "reduce", Mode: "reduce_fields", Calculations: []string{"mean"}}
	if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{single})); err != nil {
		t.Fatalf("reduce fields with calculations reject: %v", err)
	}
}

// Group by 与 Grafana 一样可以一次选多个计算方式，并给合并结果另起名字。
func TestDashboardGroupByTransformationAcceptsMultipleCalculationsAndAlias(t *testing.T) {
	transformation := DashboardTransformation{Kind: "group_by", GroupBy: "metric", Calculations: []string{"sum", "max"}, Alias: "Cluster CPU"}
	if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
		t.Fatalf("group by with multiple calculations rejected: %v", err)
	}
}

// group_by 的分组字段与聚合方式各自两两组合都必须被接受。
func TestDashboardGroupByTransformationAcceptsFieldsAndCalculations(t *testing.T) {
	for _, field := range dashboardTransformationGroupFields {
		for _, calculation := range dashboardTransformationGroupCalculations {
			transformation := DashboardTransformation{Kind: "group_by", GroupBy: field, Calculation: calculation}
			if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
				t.Fatalf("group by %s/%s rejected: %v", field, calculation, err)
			}
		}
	}
}

// 标签过滤单列一条：每块面板最多 4 条变换，四种老种类已经占满一条链。
func TestDashboardLabelFilterTransformationAcceptsPatternAndInvert(t *testing.T) {
	for _, transformation := range []DashboardTransformation{
		{Kind: "filter_by_label", Pattern: "^node-"},
		{Kind: "filter_by_label", Pattern: ".*", Invert: true},
	} {
		if err := ValidateDashboard(transformationDefinition([]DashboardTransformation{transformation})); err != nil {
			t.Fatalf("label filter %+v rejected: %v", transformation, err)
		}
	}
}

func TestDashboardTransformationsDefaultToIdentity(t *testing.T) {
	for _, transformations := range [][]DashboardTransformation{nil, {}} {
		if err := ValidateDashboard(transformationDefinition(transformations)); err != nil {
			t.Fatalf("absent transformations must stay valid: %v", err)
		}
	}
}

func TestDashboardTransformationsRejectInvalidShapes(t *testing.T) {
	cases := map[string][]DashboardTransformation{
		"too many":            make([]DashboardTransformation, MaxDashboardTransformations+1),
		"unknown kind":        {{Kind: "organize"}},
		"empty kind":          {{}},
		"pattern":             {{Kind: "rename_by_regex", Replacement: "x"}},
		"pattern regex":       {{Kind: "rename_by_regex", Pattern: "([", Replacement: "x"}},
		"pattern bytes":       {{Kind: "rename_by_regex", Pattern: strings.Repeat("a", MaxTransformationPatternBytes+1)}},
		"replacement":         {{Kind: "rename_by_regex", Pattern: "a", Replacement: strings.Repeat("b", MaxTransformationReplacementBytes+1)}},
		"control char":        {{Kind: "rename_by_regex", Pattern: "a", Replacement: "x\u0000"}},
		"rename extra":        {{Kind: "rename_by_regex", Pattern: "a", Count: intPointer(1)}},
		"comparison":          {{Kind: "filter_by_value", Comparison: "above", Value: floatPointer(1)}},
		"filter value":        {{Kind: "filter_by_value", Comparison: "gt"}},
		"filter extra":        {{Kind: "filter_by_value", Comparison: "gt", Value: floatPointer(1), Order: "asc"}},
		"statistic":           {{Kind: "sort_by", Statistic: "median", Order: "asc"}},
		"order":               {{Kind: "sort_by", Statistic: "last", Order: "up"}},
		"sort extra":          {{Kind: "sort_by", Statistic: "last", Order: "asc", Value: floatPointer(1)}},
		"count missing":       {{Kind: "limit"}},
		"count zero":          {{Kind: "limit", Count: intPointer(0)}},
		"count high":          {{Kind: "limit", Count: intPointer(MaxDashboardSeries + 1)}},
		"limit extra":         {{Kind: "limit", Count: intPointer(1), Pattern: "a"}},
		"label pattern":       {{Kind: "filter_by_label"}},
		"label regex":         {{Kind: "filter_by_label", Pattern: "(["}},
		"label bytes":         {{Kind: "filter_by_label", Pattern: strings.Repeat("a", MaxTransformationPatternBytes+1)}},
		"label extra":         {{Kind: "filter_by_label", Pattern: "a", Replacement: "b"}},
		"rename invert":       {{Kind: "rename_by_regex", Pattern: "a", Invert: true}},
		"filter invert":       {{Kind: "filter_by_value", Comparison: "gt", Value: floatPointer(1), Invert: true}},
		"sort invert":         {{Kind: "sort_by", Statistic: "last", Order: "asc", Invert: true}},
		"limit invert":        {{Kind: "limit", Count: intPointer(1), Invert: true}},
		"binary left":         {{Kind: "binary_operation", Operator: "+", Right: "b"}},
		"binary right":        {{Kind: "binary_operation", Left: "a", Operator: "+"}},
		"binary left bytes":   {{Kind: "binary_operation", Left: strings.Repeat("a", MaxTransformationPatternBytes+1), Operator: "+", Right: "b"}},
		"binary left regex":   {{Kind: "binary_operation", Left: "/(/", Operator: "+", Right: "b"}},
		"binary control":      {{Kind: "binary_operation", Left: "a\u0000", Operator: "+", Right: "b"}},
		"binary operator":     {{Kind: "binary_operation", Left: "a", Operator: "%", Right: "b"}},
		"binary alias":        {{Kind: "binary_operation", Left: "a", Operator: "+", Right: "b", Alias: strings.Repeat("c", MaxTransformationReplacementBytes+1)}},
		"binary pattern":      {{Kind: "binary_operation", Left: "a", Operator: "+", Right: "b", Pattern: "a"}},
		"binary count":        {{Kind: "binary_operation", Left: "a", Operator: "+", Right: "b", Count: intPointer(1)}},
		"binary value":        {{Kind: "binary_operation", Left: "a", Operator: "+", Right: "b", Value: floatPointer(1)}},
		"binary invert":       {{Kind: "binary_operation", Left: "a", Operator: "+", Right: "b", Invert: true}},
		"reduce missing":      {{Kind: "reduce"}},
		"reduce empty":        {{Kind: "reduce", Calculation: ""}},
		"reduce unknown":      {{Kind: "reduce", Calculation: "median"}},
		"reduce pattern":      {{Kind: "reduce", Calculation: "mean", Pattern: "a"}},
		"reduce statistic":    {{Kind: "reduce", Calculation: "mean", Statistic: "last"}},
		"reduce count":        {{Kind: "reduce", Calculation: "mean", Count: intPointer(1)}},
		"reduce invert":       {{Kind: "reduce", Calculation: "mean", Invert: true}},
		"reduce selector":     {{Kind: "reduce", Calculation: "mean", Left: "a"}},
		"reduce value":        {{Kind: "reduce", Calculation: "mean", Value: floatPointer(1)}},
		"reduce both forms":   {{Kind: "reduce", Calculation: "mean", Calculations: []string{"max"}}},
		"reduce duplicate":    {{Kind: "reduce", Mode: "series_to_rows", Calculations: []string{"min", "min"}}},
		"reduce too many":     {{Kind: "reduce", Mode: "series_to_rows", Calculations: []string{"min", "max", "mean", "sum", "count"}}},
		"reduce unknown new":  {{Kind: "reduce", Mode: "series_to_rows", Calculations: []string{"median"}}},
		"reduce mode":         {{Kind: "reduce", Mode: "median", Calculations: []string{"mean"}}},
		"reduce multi fields": {{Kind: "reduce", Mode: "reduce_fields", Calculations: []string{"min", "max"}}},
		"reduce group field":  {{Kind: "reduce", Calculation: "mean", GroupBy: "metric"}},
		"group field":         {{Kind: "group_by", GroupBy: "label", Calculation: "sum"}},
		"group field empty":   {{Kind: "group_by", Calculation: "sum"}},
		"group calculation":   {{Kind: "group_by", GroupBy: "metric", Calculation: "range"}},
		"group missing calc":  {{Kind: "group_by", GroupBy: "metric"}},
		"group pattern":       {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Pattern: "a"}},
		"group count":         {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Count: intPointer(1)}},
		"group selector":      {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Left: "a"}},
		"group mode":          {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Mode: "series_to_rows"}},
		"group both forms":    {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Calculations: []string{"max"}}},
		"group duplicate":     {{Kind: "group_by", GroupBy: "metric", Calculations: []string{"sum", "sum"}}},
		"group too many":      {{Kind: "group_by", GroupBy: "metric", Calculations: []string{"sum", "mean", "min", "max", "count"}}},
		"group unknown new":   {{Kind: "group_by", GroupBy: "metric", Calculations: []string{"delta"}}},
		"group alias bytes":   {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Alias: strings.Repeat("a", MaxTransformationReplacementBytes+1)}},
		"group alias control": {{Kind: "group_by", GroupBy: "metric", Calculation: "sum", Alias: "a\u0000"}},
	}
	for name, transformations := range cases {
		if err := ValidateDashboard(transformationDefinition(transformations)); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("%s: expected ErrInvalidDashboard, got %v", name, err)
		}
	}
}
