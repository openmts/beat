package model

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidDashboard = errors.New("invalid metric dashboard")

var (
	dashboardKeyPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	dashboardMetricPattern   = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)
	dashboardVariablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// 图例取值、排序与提示模式沿用 Grafana 的命名，便于看板在两侧互相理解。
var (
	dashboardLegendValues = []string{"last", "min", "max", "mean", "sum", "count"}
	dashboardLegendSorts  = []string{"none", "asc", "desc"}
	dashboardTooltips     = []string{"all", "single", "off"}
)

// ValidateDashboard 校验看板定义。校验前会把必须是非空数组形状的面板字段（series、thresholds）
// 补齐成空数组：这两个字段没有 omitempty，nil 切片会编码成 null，而工作区把它们当数组使用，
// null 会让整个工作区渲染失败。definition 与调用方共享同一个 panels 底层数组，所以补齐结果对
// 调用方随后落库或回传的 JSON 同样生效。
func ValidateDashboard(definition DashboardDefinition) error {
	if err := validateDashboardMetadata(definition); err != nil {
		return err
	}
	if err := validateDashboardTags(definition.Tags); err != nil {
		return err
	}
	if err := validateDashboardLinks(definition.Links); err != nil {
		return err
	}
	if err := validateDashboardPoints(definition.MaxPoints); err != nil {
		return err
	}
	if err := validateDashboardNowDelay(definition.NowDelaySeconds); err != nil {
		return err
	}
	variables, err := validateDashboardVariables(definition.Variables)
	if err != nil {
		return err
	}
	if len(definition.Panels) > MaxDashboardPanels {
		return fmt.Errorf("%w: at most %d panels are allowed", ErrInvalidDashboard, MaxDashboardPanels)
	}
	ids := make(map[string]bool, len(definition.Panels))
	for index := range definition.Panels {
		panel := &definition.Panels[index]
		normalizeDashboardPanelArrays(panel)
		if ids[panel.ID] {
			return fmt.Errorf("%w: panel IDs must be unique", ErrInvalidDashboard)
		}
		ids[panel.ID] = true
		if err := validateDashboardPanel(*panel, variables); err != nil {
			return err
		}
	}
	return nil
}

func validateDashboardMetadata(definition DashboardDefinition) error {
	if definition.Version != DashboardVersion || !dashboardText(definition.Title, 120) {
		return fmt.Errorf("%w: version or title is invalid", ErrInvalidDashboard)
	}
	if definition.Panels == nil || definition.Variables == nil {
		return fmt.Errorf("%w: panels and variables must be arrays", ErrInvalidDashboard)
	}
	if !validDashboardDescription(definition.Description) {
		return fmt.Errorf("%w: description is invalid", ErrInvalidDashboard)
	}
	if definition.TimeRangeSeconds < 60 || definition.TimeRangeSeconds > MaxDashboardRangeSeconds {
		return fmt.Errorf("%w: time range must be 1 minute to 31 days", ErrInvalidDashboard)
	}
	if !slices.Contains([]int{0, 5, 10, 15, 30, 60, 300}, definition.RefreshSeconds) {
		return fmt.Errorf("%w: refresh interval is invalid", ErrInvalidDashboard)
	}
	return nil
}

func validDashboardDescription(value string) bool {
	return len(value) <= 2048 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validateDashboardVariables(values []DashboardVariable) (map[string]bool, error) {
	if len(values) > MaxDashboardVariables {
		return nil, fmt.Errorf("%w: too many variables", ErrInvalidDashboard)
	}
	variables := make(map[string]bool, len(values))
	for _, variable := range values {
		if !dashboardVariablePattern.MatchString(variable.Name) || reservedDashboardKey(variable.Name) || variables[variable.Name] {
			return nil, fmt.Errorf("%w: variable names must be valid and unique", ErrInvalidDashboard)
		}
		if !slices.Contains([]string{"node", "group"}, variable.Type) || !dashboardText(variable.Label, 80) {
			return nil, fmt.Errorf("%w: variable type or label is invalid", ErrInvalidDashboard)
		}
		if variable.IncludeAll && !variable.Multi {
			return nil, fmt.Errorf("%w: variable All option requires multi-select", ErrInvalidDashboard)
		}
		if !validDashboardVariableRegex(variable.Regex) {
			return nil, fmt.Errorf("%w: variable regex is invalid", ErrInvalidDashboard)
		}
		variables[variable.Name] = true
	}
	return variables, nil
}

// validDashboardVariableRegex 允许空正则；否则必须是长度受限且可编译的 RE2 表达式。
// 服务端是权威：不支持 lookaround 与反向引用，与前端过滤使用同一组候选值。
func validDashboardVariableRegex(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > MaxDashboardVariableRegexBytes {
		return false
	}
	_, err := regexp.Compile(value)
	return err == nil
}

func validateDashboardPanel(panel DashboardPanel, variables map[string]bool) error {
	if !dashboardKeyPattern.MatchString(panel.ID) || reservedDashboardKey(panel.ID) || !dashboardText(panel.Title, 120) {
		return fmt.Errorf("%w: panel ID or title is invalid", ErrInvalidDashboard)
	}
	if !slices.Contains(dashboardPanelTypes, panel.Type) ||
		!slices.Contains([]string{"small", "medium", "large"}, panel.Size) {
		return fmt.Errorf("%w: panel type or size is invalid", ErrInvalidDashboard)
	}
	if err := validateDashboardStatOptions(panel); err != nil {
		return err
	}
	if err := validateDashboardLegend(panel); err != nil {
		return err
	}
	if err := validateDashboardPanelDescription(panel); err != nil {
		return err
	}
	if err := validateDashboardRow(panel); err != nil {
		return err
	}
	if err := validateDashboardPanelLinks(panel); err != nil {
		return err
	}
	if err := validateDashboardTextPanel(panel); err != nil {
		return err
	}
	if err := validateDashboardSparkline(panel); err != nil {
		return err
	}
	if err := validateDashboardTable(panel); err != nil {
		return err
	}
	if panel.Type == "row" || panel.Type == "text" {
		// 行面板只做分组、文本面板只做展示，两者都不查询数据，系列与可视化校验不适用。
		return nil
	}
	if err := validateDashboardAxes(panel); err != nil {
		return err
	}
	if err := validateDashboardAxisScale(panel); err != nil {
		return err
	}
	if err := validateDashboardAxisLabel(panel); err != nil {
		return err
	}
	if err := validateDashboardAxisDisplay(panel); err != nil {
		return err
	}
	if panel.Repeat != "" && (!dashboardVariablePattern.MatchString(panel.Repeat) || !variables[panel.Repeat]) {
		return fmt.Errorf("%w: panel repeat must reference a declared variable", ErrInvalidDashboard)
	}
	if err := validateDashboardTimeOverride(panel); err != nil {
		return err
	}
	if err := validateDashboardTimeShift(panel); err != nil {
		return err
	}
	if err := validateDashboardPoints(panel.MaxPoints); err != nil {
		return err
	}
	if err := validateDashboardThresholds(panel.Thresholds); err != nil {
		return err
	}
	if err := validateDashboardGaugeThresholds(panel); err != nil {
		return err
	}
	if err := validateDashboardThresholdMode(panel); err != nil {
		return err
	}
	if err := validateDashboardLineStyle(panel); err != nil {
		return err
	}
	if err := validateDashboardBarGauge(panel); err != nil {
		return err
	}
	if err := validateDashboardBarChart(panel); err != nil {
		return err
	}
	if err := validateDashboardPieChart(panel); err != nil {
		return err
	}
	if err := validateDashboardHeatmap(panel); err != nil {
		return err
	}
	if err := validateDashboardHistogram(panel); err != nil {
		return err
	}
	if err := validateDashboardStateTimeline(panel); err != nil {
		return err
	}
	if err := validateDashboardGeomap(panel); err != nil {
		return err
	}
	if err := validateDashboardValueMappings(panel); err != nil {
		return err
	}
	if err := validateDashboardCalculation(panel); err != nil {
		return err
	}
	if err := validateDashboardTransformations(panel); err != nil {
		return err
	}
	if err := validateDashboardNoValue(panel); err != nil {
		return err
	}
	if err := validateDashboardOverrides(panel); err != nil {
		return err
	}
	if err := validateDashboardLegend(panel); err != nil {
		return err
	}
	return validateDashboardSeries(panel, variables)
}

func validateDashboardAxes(panel DashboardPanel) error {
	if !validDashboardDecimals(panel.Decimals) {
		return fmt.Errorf("%w: decimal places must be between 0 and 6", ErrInvalidDashboard)
	}
	if panel.Min != nil && !isFinite(*panel.Min) || panel.Max != nil && !isFinite(*panel.Max) {
		return fmt.Errorf("%w: axis limits must be finite", ErrInvalidDashboard)
	}
	if panel.Min != nil && panel.Max != nil && *panel.Min >= *panel.Max {
		return fmt.Errorf("%w: axis minimum must be below maximum", ErrInvalidDashboard)
	}
	if panel.SoftMin != nil && !isFinite(*panel.SoftMin) || panel.SoftMax != nil && !isFinite(*panel.SoftMax) {
		return fmt.Errorf("%w: soft axis limits must be finite", ErrInvalidDashboard)
	}
	if panel.SoftMin != nil && panel.SoftMax != nil && *panel.SoftMin >= *panel.SoftMax {
		return fmt.Errorf("%w: soft axis minimum must be below maximum", ErrInvalidDashboard)
	}
	if panel.SoftMin != nil || panel.SoftMax != nil {
		if panel.Type != "timeseries" && panel.Type != "barchart" {
			return fmt.Errorf("%w: soft axis limits are only available on time series and bar chart panels", ErrInvalidDashboard)
		}
		// 同一侧已经有固定上下限时渲染器只会用固定值，柔性上下限永远不会生效，因此直接拒绝。
		if panel.SoftMin != nil && panel.Min != nil || panel.SoftMax != nil && panel.Max != nil {
			return fmt.Errorf("%w: soft axis limits need an automatic axis limit on the same side", ErrInvalidDashboard)
		}
	}
	return nil
}

func validDashboardDecimals(value *int) bool {
	return value == nil || (*value >= 0 && *value <= 6)
}

func validateDashboardThresholds(thresholds []DashboardThreshold) error {
	if len(thresholds) > MaxDashboardThresholds {
		return fmt.Errorf("%w: too many thresholds", ErrInvalidDashboard)
	}
	for index, threshold := range thresholds {
		if !isFinite(threshold.Value) || !dashboardColor(threshold.Color) {
			return fmt.Errorf("%w: threshold value or color is invalid", ErrInvalidDashboard)
		}
		if index > 0 && threshold.Value <= thresholds[index-1].Value {
			return fmt.Errorf("%w: thresholds must increase", ErrInvalidDashboard)
		}
	}
	return nil
}

// 图例取值与排序只在图例可见时生效，隐藏图例却带取值会静默失效，因此直接拒绝。
func validateDashboardLegend(panel DashboardPanel) error {
	sort := panel.LegendSort
	if sort == "" {
		sort = "none"
	}
	tooltip := panel.Tooltip
	if tooltip == "" {
		tooltip = "all"
	}
	if !slices.Contains(dashboardLegendSorts, sort) || !slices.Contains(dashboardTooltips, tooltip) {
		return fmt.Errorf("%w: legend sort or tooltip is invalid", ErrInvalidDashboard)
	}
	if len(panel.LegendValues) > MaxDashboardLegendValues {
		return fmt.Errorf("%w: too many legend values", ErrInvalidDashboard)
	}
	seen := make(map[string]bool, len(panel.LegendValues))
	for _, value := range panel.LegendValues {
		if !slices.Contains(dashboardLegendValues, value) || seen[value] {
			return fmt.Errorf("%w: legend values must be unique and known", ErrInvalidDashboard)
		}
		seen[value] = true
	}
	if !panel.Legend && (len(panel.LegendValues) > 0 || sort != "none") {
		return fmt.Errorf("%w: legend values require a visible legend", ErrInvalidDashboard)
	}
	// 图例位置空值等于 bottom，历史看板不受影响；显式声明时必须有可见图例且只出现在会画图例的面板上。
	if panel.LegendPlacement != "" {
		if !panel.Legend {
			return fmt.Errorf("%w: legend placement requires a visible legend", ErrInvalidDashboard)
		}
		if panel.Type != "timeseries" && panel.Type != "barchart" {
			return fmt.Errorf("%w: legend placement is only available on timeseries and bar chart panels", ErrInvalidDashboard)
		}
		if !slices.Contains(dashboardLegendPlacements, panel.LegendPlacement) {
			return fmt.Errorf("%w: legend placement is invalid", ErrInvalidDashboard)
		}
	}
	// 右侧图例列宽依附 placement=right：bottom 布局没有固定列，宽度写了也不会渲染。
	if err := validateDashboardLegendWidth(panel); err != nil {
		return err
	}
	return nil
}

// validateDashboardLegendWidth 校验 Grafana Legend → Width。空值表示作者没有声明，
// 沿用渲染器缺省的 160px；显式声明时必须有可见图例、位置在右侧，且只出现在会画图例的面板上。
func validateDashboardLegendWidth(panel DashboardPanel) error {
	if panel.LegendWidth == nil {
		return nil
	}
	if !panel.Legend {
		return fmt.Errorf("%w: legend width requires a visible legend", ErrInvalidDashboard)
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: legend width is only available on timeseries and bar chart panels", ErrInvalidDashboard)
	}
	if panel.LegendPlacement != "right" {
		return fmt.Errorf("%w: legend width needs the legend placed on the right", ErrInvalidDashboard)
	}
	if *panel.LegendWidth < MinDashboardLegendWidth || *panel.LegendWidth > MaxDashboardLegendWidth {
		return fmt.Errorf("%w: legend width must be between %d and %d", ErrInvalidDashboard, MinDashboardLegendWidth, MaxDashboardLegendWidth)
	}
	return nil
}

func validateDashboardSeries(panel DashboardPanel, variables map[string]bool) error {
	if len(panel.Series) < 1 || len(panel.Series) > MaxDashboardSeries {
		return fmt.Errorf("%w: panels require 1 to 8 series", ErrInvalidDashboard)
	}
	if (panel.Type == "stat" || panel.Type == "gauge") && len(panel.Series) != 1 {
		return fmt.Errorf("%w: stat and gauge panels require exactly one series", ErrInvalidDashboard)
	}
	seen := make(map[string]bool, len(panel.Series))
	for _, series := range panel.Series {
		if seen[series.RefID] {
			return fmt.Errorf("%w: series references must be unique", ErrInvalidDashboard)
		}
		seen[series.RefID] = true
		if err := validateDashboardSeriesValue(series, variables); err != nil {
			return err
		}
	}
	return nil
}

func validateDashboardSeriesValue(series DashboardSeries, variables map[string]bool) error {
	if !validDashboardDevice(series) {
		return ErrInvalidDashboard
	}
	if !validDashboardSeriesIdentity(series) {
		return fmt.Errorf("%w: series reference or metric is invalid", ErrInvalidDashboard)
	}
	if !slices.Contains([]string{"avg", "min", "max", "sum", "last", "rate"}, series.Aggregation) {
		return fmt.Errorf("%w: aggregation is invalid", ErrInvalidDashboard)
	}
	if !dashboardColor(series.Color) || series.Label != "" && !dashboardText(series.Label, 120) {
		return fmt.Errorf("%w: series label or color is invalid", ErrInvalidDashboard)
	}
	if strings.HasPrefix(series.NodeID, "$") {
		if !variables[strings.TrimPrefix(series.NodeID, "$")] {
			return fmt.Errorf("%w: series variable is not defined", ErrInvalidDashboard)
		}
	} else if !dashboardKeyPattern.MatchString(series.NodeID) {
		return fmt.Errorf("%w: node reference is invalid", ErrInvalidDashboard)
	}
	return nil
}

func validDashboardSeriesIdentity(series DashboardSeries) bool {
	return dashboardKeyPattern.MatchString(series.RefID) && !reservedDashboardKey(series.RefID) && dashboardMetricPattern.MatchString(series.Metric)
}

func dashboardColor(value string) bool {
	return slices.Contains([]string{"blue", "green", "amber", "rose", "violet", "cyan", "slate"}, value)
}

func reservedDashboardKey(value string) bool {
	return slices.Contains([]string{"__proto__", "prototype", "constructor"}, value)
}

func dashboardText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func validDashboardDevice(series DashboardSeries) bool {
	if ValidAcceleratorMetric(series.Metric) {
		return ValidAcceleratorDeviceID(series.DeviceID) && !strings.HasPrefix(series.NodeID, "$")
	}
	return series.DeviceID == ""
}

// normalizeDashboardPanelArrays 把 nil 的数组字段补成空数组，保证编码出的 JSON 与工作区
// 期望的数组形状一致（空数组），而不是 null。
func normalizeDashboardPanelArrays(panel *DashboardPanel) {
	if panel.Series == nil {
		panel.Series = []DashboardSeries{}
	}
	if panel.Thresholds == nil {
		panel.Thresholds = []DashboardThreshold{}
	}
}
