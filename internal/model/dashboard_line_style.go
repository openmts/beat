package model

import (
	"fmt"
	"slices"
)

const (
	// MinDashboardLineWidth/MaxDashboardLineWidth 是折线宽度的闭区间。
	MinDashboardLineWidth     = 1
	MaxDashboardLineWidth     = 4
	DefaultDashboardLineWidth = 2
	// MaxDashboardFillOpacity 是折线下方填充的不透明度上限（百分比）。
	MaxDashboardFillOpacity = 100
	// MinDashboardPointSize/MaxDashboardPointSize 是数据点标记半径的闭区间（Grafana 的 Point size）。
	MinDashboardPointSize = 1
	MaxDashboardPointSize = 12
)

// 折线样式沿用 Grafana 的命名，便于看板在两侧互相理解。
var (
	dashboardShowPointsChoices = []string{"auto", "always", "never"}
	// dashboardStackModes 是堆叠模式：none 不堆叠、normal 按原始值堆叠、percent 把每个时间点归一化成 100%。
	dashboardStackModes = []string{"none", "normal", "percent"}
	// dashboardGradientModes 是填充渐变取值：none 是均匀填充，opacity 与 hue 才画渐变。
	dashboardGradientModes = []string{"none", "opacity", "hue"}
	// dashboardLineInterpolations 是折线插值取值，与渲染器内置的四种曲线一一对应；
	// step_before/step_after 分别对应「先保持后跳变」与「先跳变后保持」。
	dashboardLineInterpolations = []string{"linear", "smooth", "step_before", "step_after"}
	// dashboardLinePatterns 是折线线型：solid 是缺省的连续线，dashed/dotted 只改变描边虚线样式。
	dashboardLinePatterns = []string{"solid", "dashed", "dotted"}
	// dashboardConnectNullsModes 是空样本处理：none 保留缺口，always 跨过缺口连线。
	dashboardConnectNullsModes = []string{"none", "always"}
)

func normalizeGradientMode(panel DashboardPanel) string {
	if panel.GradientMode == "" {
		return "none"
	}
	return panel.GradientMode
}

// effectiveFillOpacity 是填充不透明度的生效值，与前端 panelLineStyle 的缺省规则一致：
// 折线缺省不填充，柱状图缺省实心，否则「一条不透明的柱子」会被看成空框。
func effectiveFillOpacity(panel DashboardPanel) int {
	if panel.FillOpacity != nil {
		return *panel.FillOpacity
	}
	if panel.Type == "barchart" {
		return MaxDashboardFillOpacity
	}
	return 0
}

// validateDashboardLineStyle 校验线宽、填充、渐变、插值、数据点与堆叠；这些字段只对 timeseries
// 与 barchart 面板有效，其它面板类型带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardLineStyle(panel DashboardPanel) error {
	if panel.LineWidth != nil && (*panel.LineWidth < MinDashboardLineWidth || *panel.LineWidth > MaxDashboardLineWidth) {
		return fmt.Errorf("%w: line width must be between %d and %d", ErrInvalidDashboard, MinDashboardLineWidth, MaxDashboardLineWidth)
	}
	if panel.FillOpacity != nil && (*panel.FillOpacity < 0 || *panel.FillOpacity > MaxDashboardFillOpacity) {
		return fmt.Errorf("%w: fill opacity must be between 0 and %d", ErrInvalidDashboard, MaxDashboardFillOpacity)
	}
	if panel.PointSize != nil && (*panel.PointSize < MinDashboardPointSize || *panel.PointSize > MaxDashboardPointSize) {
		return fmt.Errorf("%w: point size must be between %d and %d", ErrInvalidDashboard, MinDashboardPointSize, MaxDashboardPointSize)
	}
	showPoints := panel.ShowPoints
	if showPoints == "" {
		showPoints = "auto"
	}
	stack := panel.Stack
	if stack == "" {
		stack = "none"
	}
	if !slices.Contains(dashboardShowPointsChoices, showPoints) || !slices.Contains(dashboardStackModes, stack) {
		return fmt.Errorf("%w: show points or stack mode is invalid", ErrInvalidDashboard)
	}
	// 百分比堆叠把每个时间点归一化成 0..100%，对数轴既放不下 0 也会让刻度失去意义，因此互斥。
	if stack == "percent" && panel.Scale == "log" {
		return fmt.Errorf("%w: percent stacking needs a linear axis", ErrInvalidDashboard)
	}
	// 柱状图与折线共用填充、渐变与堆叠（Grafana 的 Bar chart 同样有 Fill opacity 与 Stacking）；
	// 线宽与数据点标记只对折线有意义，柱状图带上它们同样会渲染不出效果，因此一并拒绝。
	if panel.Type == "barchart" {
		if panel.LineWidth != nil || panel.ShowPoints != "" || panel.PointSize != nil || panel.LineInterpolation != "" ||
			panel.LinePattern != "" || panel.ConnectNulls != "" {
			return fmt.Errorf("%w: line width, data points, line interpolation, line pattern and null handling are only available on timeseries panels", ErrInvalidDashboard)
		}
		return validateDashboardGradient(panel)
	}
	if panel.Type != "timeseries" && (panel.LineWidth != nil || panel.FillOpacity != nil || panel.ShowPoints != "" ||
		panel.PointSize != nil || panel.Stack != "" || panel.GradientMode != "" || panel.LineInterpolation != "" ||
		panel.LinePattern != "" || panel.ConnectNulls != "") {
		return fmt.Errorf("%w: line style is only available on timeseries panels", ErrInvalidDashboard)
	}
	if panel.LineInterpolation != "" && !slices.Contains(dashboardLineInterpolations, panel.LineInterpolation) {
		return fmt.Errorf("%w: line interpolation is invalid", ErrInvalidDashboard)
	}
	if panel.LinePattern != "" && !slices.Contains(dashboardLinePatterns, panel.LinePattern) {
		return fmt.Errorf("%w: line pattern is invalid", ErrInvalidDashboard)
	}
	if panel.ConnectNulls != "" && !slices.Contains(dashboardConnectNullsModes, panel.ConnectNulls) {
		return fmt.Errorf("%w: null handling is invalid", ErrInvalidDashboard)
	}
	// 点大小只在不画点时才会被渲染器忽略，因此 show_points=never 下带上它同样按无效看板拒绝。
	if panel.PointSize != nil && showPoints == "never" {
		return fmt.Errorf("%w: point size requires visible data points", ErrInvalidDashboard)
	}
	return validateDashboardGradient(panel)
}

// validateDashboardGradient 校验填充渐变：取值只能是 none/opacity/hue，且必须有可渐变的填充，
// 否则配置写了也渲染不出效果。
func validateDashboardGradient(panel DashboardPanel) error {
	gradient := normalizeGradientMode(panel)
	if !slices.Contains(dashboardGradientModes, gradient) {
		return fmt.Errorf("%w: gradient mode is invalid", ErrInvalidDashboard)
	}
	if gradient != "none" && effectiveFillOpacity(panel) == 0 {
		return fmt.Errorf("%w: gradient mode requires a non-zero fill opacity", ErrInvalidDashboard)
	}
	return nil
}
