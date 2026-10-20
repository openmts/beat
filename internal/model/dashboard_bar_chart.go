package model

import (
	"fmt"
	"slices"
)

const (
	// MinDashboardBarWidth/MaxDashboardBarWidth 是单根柱子的最大宽度（像素）。
	MinDashboardBarWidth     = 4
	MaxDashboardBarWidth     = 40
	DefaultDashboardBarWidth = 12
	// MinDashboardBarRadius/MaxDashboardBarRadius 是柱顶圆角半径（像素）。
	MinDashboardBarRadius = 0
	MaxDashboardBarRadius = 10
)

// dashboardBarAlignments 是 Grafana 的 Bar alignment 取值：auto 是缺省，等同于 centered。
var dashboardBarAlignments = []string{"auto", "before", "centered", "after"}

// validateDashboardBarChart 校验柱宽、柱圆角与柱对齐。三者只对 barchart 面板有效：其它面板类型带上它们
// 渲染不出效果，属于「配置写了但永远不生效」的看板，因此直接拒绝而不是静默忽略。
func validateDashboardBarChart(panel DashboardPanel) error {
	if panel.BarRadius != nil && (*panel.BarRadius < MinDashboardBarRadius || *panel.BarRadius > MaxDashboardBarRadius) {
		return fmt.Errorf("%w: bar radius must be between %d and %d", ErrInvalidDashboard, MinDashboardBarRadius, MaxDashboardBarRadius)
	}
	if panel.BarWidth != nil && (*panel.BarWidth < MinDashboardBarWidth || *panel.BarWidth > MaxDashboardBarWidth) {
		return fmt.Errorf("%w: bar width must be between %d and %d", ErrInvalidDashboard, MinDashboardBarWidth, MaxDashboardBarWidth)
	}
	if panel.Type != "barchart" && (panel.BarWidth != nil || panel.BarRadius != nil || panel.BarAlignment != "") {
		return fmt.Errorf("%w: bar chart options are only available on bar chart panels", ErrInvalidDashboard)
	}
	if panel.BarAlignment != "" && !slices.Contains(dashboardBarAlignments, panel.BarAlignment) {
		return fmt.Errorf("%w: bar alignment is invalid", ErrInvalidDashboard)
	}
	return nil
}
