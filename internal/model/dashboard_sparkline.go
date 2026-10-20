package model

import (
	"fmt"
	"slices"
)

const (
	// MinDashboardSparklineLineWidth/MaxDashboardSparklineLineWidth 是迷你趋势图线宽的闭区间，
	// DefaultDashboardSparklineLineWidth 是未声明时的线宽。字段独立于折线的 LineWidth，
	// 因为 stat 面板的迷你趋势图只在值旁边作陪衬，语义与整块折线画布不同。
	MinDashboardSparklineLineWidth     = 1
	MaxDashboardSparklineLineWidth     = 4
	DefaultDashboardSparklineLineWidth = 2
	// MaxDashboardSparklineFillOpacity 是迷你趋势图下方填充的不透明度上限（百分比）。
	MaxDashboardSparklineFillOpacity = 100
)

// sparklineFillOpacity 是迷你趋势图填充的生效值：未声明等于 0，与前端 sparklineStyle 的缺省规则一致。
func sparklineFillOpacity(panel DashboardPanel) int {
	if panel.SparklineFillOpacity != nil {
		return *panel.SparklineFillOpacity
	}
	return 0
}

// validateDashboardSparkline 校验 stat 面板的迷你趋势图（Grafana 的 Value options → Sparkline）
// 及其线宽、填充与渐变。nil 表示作者没有声明，沿用历史行为（不画趋势图）；显式打开或关闭只允许出现在
// stat 面板上，其它类型带上它会渲染不出效果，因此直接拒绝。其余选项只有在趋势图打开时才有效，
// 否则就是「配置写了但永远不渲染」的看板。
func validateDashboardSparkline(panel DashboardPanel) error {
	if panel.Sparkline == nil && panel.SparklineLineWidth == nil && panel.SparklineFillOpacity == nil &&
		panel.SparklineGradientMode == "" {
		return nil
	}
	if panel.Type != "stat" {
		return fmt.Errorf("%w: sparkline is only available on stat panels", ErrInvalidDashboard)
	}
	if panel.Sparkline == nil || !*panel.Sparkline {
		if panel.SparklineLineWidth != nil || panel.SparklineFillOpacity != nil || panel.SparklineGradientMode != "" {
			return fmt.Errorf("%w: sparkline line width, fill opacity and gradient mode require an enabled sparkline", ErrInvalidDashboard)
		}
		return nil
	}
	if panel.SparklineLineWidth != nil && (*panel.SparklineLineWidth < MinDashboardSparklineLineWidth || *panel.SparklineLineWidth > MaxDashboardSparklineLineWidth) {
		return fmt.Errorf("%w: sparkline line width must be between %d and %d", ErrInvalidDashboard, MinDashboardSparklineLineWidth, MaxDashboardSparklineLineWidth)
	}
	if panel.SparklineFillOpacity != nil && (*panel.SparklineFillOpacity < 0 || *panel.SparklineFillOpacity > MaxDashboardSparklineFillOpacity) {
		return fmt.Errorf("%w: sparkline fill opacity must be between 0 and %d", ErrInvalidDashboard, MaxDashboardSparklineFillOpacity)
	}
	// 渐变与折线共用同一套枚举（none/opacity/hue），空值等于 none；非 none 时必须有可渐变的填充。
	gradient := panel.SparklineGradientMode
	if gradient == "" {
		gradient = "none"
	}
	if !slices.Contains(dashboardGradientModes, gradient) {
		return fmt.Errorf("%w: sparkline gradient mode is invalid", ErrInvalidDashboard)
	}
	if gradient != "none" && sparklineFillOpacity(panel) == 0 {
		return fmt.Errorf("%w: sparkline gradient mode requires a non-zero fill opacity", ErrInvalidDashboard)
	}
	return nil
}
