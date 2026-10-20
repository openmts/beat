package model

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// 字段覆盖沿用 Grafana 的 Overrides 标签页：先按匹配器选中序列，再覆盖这些序列的显示属性。
// 属性集合刻意保持封闭——只有渲染层真正消费的属性才允许出现，避免保存出「配置写了但不生效」的看板。
const (
	// MaxDashboardOverrides 是单个面板允许的覆盖条数。
	MaxDashboardOverrides = 16
	// MaxDashboardOverrideUnitBytes 限制覆盖里显式给出的单位键长度。
	MaxDashboardOverrideUnitBytes = 32
	// MaxDashboardOverrideFillBelowBytes 限制「填充到另一条序列」里给出的序列名称长度。
	MaxDashboardOverrideFillBelowBytes = 128
	// MaxDashboardOverrideDisplayNameBytes 限制覆盖里的显示名长度（Grafana 的 Display name）。
	MaxDashboardOverrideDisplayNameBytes = 128
	// MinDashboardAxisWidth/MaxDashboardAxisWidth 是逐序列轴宽的闭区间：与 Grafana 的 Axis → Width
	// 一样 0 表示自动（按单位与轴标题算），上限挡住把绘图区压没的配置。
	MinDashboardAxisWidth = 0
	MaxDashboardAxisWidth = 500
)

var (
	dashboardOverrideMatcherTypes = []string{"by_name", "by_regex", "by_ref_id", "by_type", "by_value"}
	dashboardOverridePropertyKeys = []string{"color", "unit", "hide_legend", "line_width", "fill_opacity", "axis_placement",
		"thresholds", "fill_below_to", "axis_label", "axis_soft_min", "axis_soft_max", "axis_width",
		"display_name", "decimals", "min", "max"}
)

// dashboardSeriesTypeTokens 是「按字段类型」匹配允许的序列类型：Beat 的序列值都是数值，
// 唯一稳定的类型维度是它的单位族（见 webui 的 workspaceUnitFamily），因此取值刻意保持封闭。
var dashboardSeriesTypeTokens = []string{"none", "number", "percent", "bytes", "rate", "duration", "other"}

// dashboardOverrideStandardTypes 列出消费逐序列 Display name / Decimals 的面板：这些面板会给序列
// 渲染名字与数值，其余类型（行、文本、热力图、直方图）带上它们只会保存出一份渲染器忽略的配置。
var dashboardOverrideStandardTypes = []string{"timeseries", "barchart", "stat", "gauge", "bargauge", "table", "statetimeline", "piechart"}

// dashboardOverrideLimitTypes 列出消费逐序列 Min / Max 的面板：只有把单值映射到一段量程的
// stat / gauge / bargauge 有值域，折线与柱状图的坐标轴范围是面板级设置，其余面板没有量程。
var dashboardOverrideLimitTypes = []string{"stat", "gauge", "bargauge"}

// dashboardOverrideThresholdTypes 列出真正会按命中序列自己的阈值取色的面板类型：其余类型带上
// thresholds 只会保存出一份渲染器忽略的配置，因此直接拒绝。
var dashboardOverrideThresholdTypes = []string{"stat", "gauge", "bargauge", "table", "statetimeline"}

// validateDashboardOverrides 逐条校验覆盖规则；覆盖只影响显示，因此没有任何跨字段约束。
func validateDashboardOverrides(panel DashboardPanel) error {
	if len(panel.Overrides) > MaxDashboardOverrides {
		return fmt.Errorf("%w: at most %d overrides are allowed", ErrInvalidDashboard, MaxDashboardOverrides)
	}
	for _, override := range panel.Overrides {
		if err := validateDashboardOverride(panel, override); err != nil {
			return err
		}
	}
	return nil
}

func validateDashboardOverride(panel DashboardPanel, override DashboardOverride) error {
	if err := validateDashboardOverrideMatcher(override.Matcher); err != nil {
		return err
	}
	if override.Color != "" && !dashboardColor(override.Color) {
		return fmt.Errorf("%w: override color is invalid", ErrInvalidDashboard)
	}
	if override.Unit != "" && !dashboardText(override.Unit, MaxDashboardOverrideUnitBytes) {
		return fmt.Errorf("%w: override unit must be printable and at most %d bytes", ErrInvalidDashboard, MaxDashboardOverrideUnitBytes)
	}
	if err := validateOverrideLineStyle(panel, override); err != nil {
		return err
	}
	if err := validateOverrideAxisPlacement(panel, override); err != nil {
		return err
	}
	if err := validateOverrideAxisOptions(panel, override); err != nil {
		return err
	}
	if err := validateOverrideThresholds(panel, override); err != nil {
		return err
	}
	if err := validateOverrideFillBelowTo(panel, override); err != nil {
		return err
	}
	if err := validateOverrideStandard(panel, override); err != nil {
		return err
	}
	if !overrideCarriesProperty(override) {
		return fmt.Errorf("%w: override needs at least one of %v", ErrInvalidDashboard, dashboardOverridePropertyKeys)
	}
	return nil
}

// validateOverrideLineStyle 让每序列的线宽与填充沿用面板级的可用范围：线宽只对折线有意义，
// 填充对折线与柱状图有意义，其余面板类型带上它们只会保存出一份渲染器忽略的配置。
func validateOverrideLineStyle(panel DashboardPanel, override DashboardOverride) error {
	if override.LineWidth != nil {
		if panel.Type != "timeseries" {
			return fmt.Errorf("%w: override line width is only available on timeseries panels", ErrInvalidDashboard)
		}
		if *override.LineWidth < MinDashboardLineWidth || *override.LineWidth > MaxDashboardLineWidth {
			return fmt.Errorf("%w: override line width must be between %d and %d", ErrInvalidDashboard, MinDashboardLineWidth, MaxDashboardLineWidth)
		}
	}
	if override.FillOpacity != nil {
		if panel.Type != "timeseries" && panel.Type != "barchart" {
			return fmt.Errorf("%w: override fill opacity is only available on timeseries and barchart panels", ErrInvalidDashboard)
		}
		if *override.FillOpacity < 0 || *override.FillOpacity > MaxDashboardFillOpacity {
			return fmt.Errorf("%w: override fill opacity must be between 0 and %d", ErrInvalidDashboard, MaxDashboardFillOpacity)
		}
	}
	return nil
}

// validateOverrideAxisPlacement 让每序列的轴位置沿用面板级的可用范围：只有折线与柱状面板有可切换的
// 值轴，其余面板类型带上它只会保存出一份渲染器忽略的配置。
func validateOverrideAxisPlacement(panel DashboardPanel, override DashboardOverride) error {
	if override.AxisPlacement == "" {
		return nil
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: override axis placement is only available on time series and bar chart panels", ErrInvalidDashboard)
	}
	if override.AxisPlacement != DashboardAxisPlacementLeft && override.AxisPlacement != DashboardAxisPlacementRight {
		return fmt.Errorf("%w: override axis placement must be left or right", ErrInvalidDashboard)
	}
	return nil
}

// validateOverrideThresholds 让每序列阈值沿用面板级的取值范围与递增约束：只有按阈值给单值取色的
// 面板消费它，值映射仍优先于阈值。
func validateOverrideThresholds(panel DashboardPanel, override DashboardOverride) error {
	if len(override.Thresholds) == 0 {
		return nil
	}
	if !slices.Contains(dashboardOverrideThresholdTypes, panel.Type) {
		return fmt.Errorf("%w: override thresholds are not available on this panel type", ErrInvalidDashboard)
	}
	return validateDashboardThresholds(override.Thresholds)
}

func validateDashboardOverrideMatcher(matcher DashboardOverrideMatcher) error {
	if !slices.Contains(dashboardOverrideMatcherTypes, matcher.Type) {
		return fmt.Errorf("%w: override matcher type must be one of %v", ErrInvalidDashboard, dashboardOverrideMatcherTypes)
	}
	if !dashboardText(matcher.Value, MaxTransformationPatternBytes) {
		return fmt.Errorf("%w: override matcher value must be printable and at most %d bytes", ErrInvalidDashboard, MaxTransformationPatternBytes)
	}
	if matcher.Type == "by_regex" {
		if _, err := regexp.Compile(matcher.Value); err != nil {
			return fmt.Errorf("%w: override matcher value must be a valid expression", ErrInvalidDashboard)
		}
	}
	// 按字段类型与按值的匹配值在同一列里是枚举与有限数，两者都提前拒绝，避免保存出永不命中的规则。
	if matcher.Type == "by_type" && !slices.Contains(dashboardSeriesTypeTokens, matcher.Value) {
		return fmt.Errorf("%w: override matcher value must be one of %v", ErrInvalidDashboard, dashboardSeriesTypeTokens)
	}
	if matcher.Type == "by_value" {
		value, err := strconv.ParseFloat(matcher.Value, 64)
		if err != nil || !isFinite(value) {
			return fmt.Errorf("%w: override matcher value must be a finite number", ErrInvalidDashboard)
		}
	}
	return nil
}

// validateOverrideStandard 校验 Grafana 的 Overrides → Standard options：Display name 与 Decimals 跟着
// 面板是否给序列渲染名字与数值，Min / Max 只在有单值量程的面板上生效，两者都按面板类型门禁。
func validateOverrideStandard(panel DashboardPanel, override DashboardOverride) error {
	if override.DisplayName == "" && override.Decimals == nil && override.Min == nil && override.Max == nil {
		return nil
	}
	if !slices.Contains(dashboardOverrideStandardTypes, panel.Type) {
		return fmt.Errorf("%w: override display name and decimals are not available on this panel type", ErrInvalidDashboard)
	}
	if override.DisplayName != "" && !dashboardText(override.DisplayName, MaxDashboardOverrideDisplayNameBytes) {
		return fmt.Errorf("%w: override display name must be printable and at most %d bytes", ErrInvalidDashboard, MaxDashboardOverrideDisplayNameBytes)
	}
	if !validDashboardDecimals(override.Decimals) {
		return fmt.Errorf("%w: override decimal places must be between 0 and 6", ErrInvalidDashboard)
	}
	if override.Min == nil && override.Max == nil {
		return nil
	}
	if !slices.Contains(dashboardOverrideLimitTypes, panel.Type) {
		return fmt.Errorf("%w: override value limits are only available on stat, gauge and bar gauge panels", ErrInvalidDashboard)
	}
	if override.Min != nil && !isFinite(*override.Min) || override.Max != nil && !isFinite(*override.Max) {
		return fmt.Errorf("%w: override value limits must be finite", ErrInvalidDashboard)
	}
	if override.Min != nil && override.Max != nil && *override.Min >= *override.Max {
		return fmt.Errorf("%w: override minimum must be below maximum", ErrInvalidDashboard)
	}
	return nil
}

// validateOverrideAxisOptions 让逐序列轴选项沿用面板级轴规则：只有折线与柱状面板有值轴，
// 柔性上下限必须是有限值且下限小于上限；面板自己声明了固定上下限时柔性值永远不会生效，直接拒绝。
func validateOverrideAxisOptions(panel DashboardPanel, override DashboardOverride) error {
	if override.AxisLabel == "" && override.AxisSoftMin == nil && override.AxisSoftMax == nil && override.AxisWidth == nil {
		return nil
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: override axis options are only available on time series and bar chart panels", ErrInvalidDashboard)
	}
	if override.AxisLabel != "" && (len(override.AxisLabel) > MaxDashboardAxisLabelBytes || !printableTransformationText(override.AxisLabel)) {
		return fmt.Errorf("%w: override axis label must be printable and at most %d bytes", ErrInvalidDashboard, MaxDashboardAxisLabelBytes)
	}
	if override.AxisSoftMin != nil && !isFinite(*override.AxisSoftMin) || override.AxisSoftMax != nil && !isFinite(*override.AxisSoftMax) {
		return fmt.Errorf("%w: override soft axis limits must be finite", ErrInvalidDashboard)
	}
	if override.AxisSoftMin != nil && override.AxisSoftMax != nil && *override.AxisSoftMin >= *override.AxisSoftMax {
		return fmt.Errorf("%w: override soft axis minimum must be below maximum", ErrInvalidDashboard)
	}
	if (override.AxisSoftMin != nil || override.AxisSoftMax != nil) && (panel.Min != nil || panel.Max != nil) {
		return fmt.Errorf("%w: override soft axis limits need an automatic panel axis limit", ErrInvalidDashboard)
	}
	if override.AxisWidth != nil && (*override.AxisWidth < MinDashboardAxisWidth || *override.AxisWidth > MaxDashboardAxisWidth) {
		return fmt.Errorf("%w: override axis width must be between %d and %d", ErrInvalidDashboard, MinDashboardAxisWidth, MaxDashboardAxisWidth)
	}
	return nil
}

// validateOverrideFillBelowTo 让「填充到另一条序列」沿用面板级的可用范围：堆叠按层叠面积绘制，
// 与「填充到另一条线」互斥，因此只有不堆叠的折线面板消费它。
func validateOverrideFillBelowTo(panel DashboardPanel, override DashboardOverride) error {
	if override.FillBelowTo == "" {
		return nil
	}
	if panel.Type != "timeseries" {
		return fmt.Errorf("%w: override fill below to is only available on time series panels", ErrInvalidDashboard)
	}
	if panel.Stack != "" && panel.Stack != "none" {
		return fmt.Errorf("%w: override fill below to is not available on stacked panels", ErrInvalidDashboard)
	}
	if !dashboardText(override.FillBelowTo, MaxDashboardOverrideFillBelowBytes) {
		return fmt.Errorf("%w: override fill below to must be printable and at most %d bytes", ErrInvalidDashboard, MaxDashboardOverrideFillBelowBytes)
	}
	return nil
}

func overrideCarriesProperty(override DashboardOverride) bool {
	return override.Color != "" || override.Unit != "" || override.HideLegend != nil ||
		override.LineWidth != nil || override.FillOpacity != nil || override.AxisPlacement != "" ||
		len(override.Thresholds) > 0 || override.FillBelowTo != "" || override.AxisLabel != "" ||
		override.AxisSoftMin != nil || override.AxisSoftMax != nil || override.AxisWidth != nil ||
		override.DisplayName != "" || override.Decimals != nil || override.Min != nil || override.Max != nil
}
