package model

import (
	"fmt"
	"slices"
)

// 条状仪表沿用 Grafana 的命名：方向、显示模式与是否绘制未填充轨道。
var (
	dashboardBarOrientations = []string{"auto", "horizontal", "vertical"}
	dashboardBarDisplays     = []string{"basic", "gradient", "lcd"}
)

// validDashboardPanelTypes 是全部可视化类型；条状仪表与饼图都只消费最新样本，但它们按序列
// 逐条绘制，因此允许多序列。
var dashboardPanelTypes = []string{"timeseries", "stat", "gauge", "table", "bargauge", "piechart", "barchart", "heatmap", "histogram", "statetimeline", "geomap", "text", "row"}

// DefaultDashboardBarOrientation 与 Grafana 的 auto 一致：单序列竖排，多序列横排。
func DefaultDashboardBarOrientation(seriesCount int) string {
	if seriesCount <= 1 {
		return "vertical"
	}
	return "horizontal"
}

// validateDashboardBarGauge 校验方向、显示模式与未填充轨道开关；这些字段只对 bargauge 生效，
// 其它面板类型带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardBarGauge(panel DashboardPanel) error {
	orientation := panel.BarOrientation
	if orientation == "" {
		orientation = "auto"
	}
	display := panel.BarDisplay
	if display == "" {
		display = "basic"
	}
	if !slices.Contains(dashboardBarOrientations, orientation) || !slices.Contains(dashboardBarDisplays, display) {
		return fmt.Errorf("%w: bar gauge orientation or display is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "bargauge" && (panel.BarOrientation != "" || panel.BarDisplay != "" || panel.BarUnfilled != nil) {
		return fmt.Errorf("%w: bar gauge options are only available on bar gauge panels", ErrInvalidDashboard)
	}
	return nil
}
