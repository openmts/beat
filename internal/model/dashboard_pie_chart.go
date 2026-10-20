package model

import (
	"fmt"
	"slices"
)

// 饼图沿用 Grafana 的命名：形态为饼图或环形图，标签可以取百分比、数值或两者。
var (
	dashboardPieChartTypes = []string{"pie", "donut"}
	dashboardPieLabelModes = []string{"percent", "value", "both"}
)

// 缺省值与 Grafana 的默认输出一致：实心饼图、只标注百分比。
const (
	dashboardDefaultPieChartType = "pie"
	dashboardDefaultPieLabelMode = "percent"
)

// DefaultDashboardPieChartType 返回缺省形态，供文档与前端保持一致。
func DefaultDashboardPieChartType() string { return dashboardDefaultPieChartType }

// DefaultDashboardPieLabelMode 返回缺省标签模式，供文档与前端保持一致。
func DefaultDashboardPieLabelMode() string { return dashboardDefaultPieLabelMode }

// validateDashboardPieChart 校验形态与标签模式；这两个字段只对 piechart 生效，其它面板类型
// 带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardPieChart(panel DashboardPanel) error {
	chartType := panel.PieChartType
	if chartType == "" {
		chartType = dashboardDefaultPieChartType
	}
	labelMode := panel.PieLabelMode
	if labelMode == "" {
		labelMode = dashboardDefaultPieLabelMode
	}
	if !slices.Contains(dashboardPieChartTypes, chartType) || !slices.Contains(dashboardPieLabelModes, labelMode) {
		return fmt.Errorf("%w: pie chart type or label mode is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "piechart" && (panel.PieChartType != "" || panel.PieLabelMode != "") {
		return fmt.Errorf("%w: pie chart options are only available on pie chart panels", ErrInvalidDashboard)
	}
	return nil
}
