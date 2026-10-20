package model

import (
	"fmt"
	"slices"
)

// 热力图沿用 Grafana 的取值轴命名：分桶数量、线性/对数刻度与基础配色。
var dashboardHeatmapScales = []string{"linear", "log"}

const (
	// MinDashboardHeatmapBuckets / MaxDashboardHeatmapBuckets 限制取值轴分桶数量：
	// 分桶过少看不出分布，分桶过多会让单元格小于一个像素并放大渲染成本。
	MinDashboardHeatmapBuckets = 2
	MaxDashboardHeatmapBuckets = 64
	// dashboardHeatmapDefaultBuckets 与 Grafana 直方图的缺省分桶数量一致。
	dashboardHeatmapDefaultBuckets = 12
	dashboardHeatmapDefaultScale   = "linear"
	dashboardHeatmapDefaultColor   = "blue"
)

// DefaultDashboardHeatmapBuckets 返回缺省分桶数量，供文档与前端保持一致。
func DefaultDashboardHeatmapBuckets() int { return dashboardHeatmapDefaultBuckets }

// DefaultDashboardHeatmapScale 返回缺省取值轴刻度。
func DefaultDashboardHeatmapScale() string { return dashboardHeatmapDefaultScale }

// DefaultDashboardHeatmapColor 返回缺省配色；它与其它面板的系列默认色一致。
func DefaultDashboardHeatmapColor() string { return dashboardHeatmapDefaultColor }

// validateDashboardHeatmap 校验分桶数量、刻度与配色；这三项只对 heatmap 生效，其它面板类型
// 带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardHeatmap(panel DashboardPanel) error {
	buckets := dashboardHeatmapDefaultBuckets
	if panel.HeatmapBuckets != nil {
		buckets = *panel.HeatmapBuckets
	}
	scale := panel.HeatmapScale
	if scale == "" {
		scale = dashboardHeatmapDefaultScale
	}
	color := panel.HeatmapColor
	if color == "" {
		color = dashboardHeatmapDefaultColor
	}
	if buckets < MinDashboardHeatmapBuckets || buckets > MaxDashboardHeatmapBuckets {
		return fmt.Errorf("%w: heatmap bucket count must be between %d and %d", ErrInvalidDashboard, MinDashboardHeatmapBuckets, MaxDashboardHeatmapBuckets)
	}
	if !slices.Contains(dashboardHeatmapScales, scale) || !dashboardColor(color) {
		return fmt.Errorf("%w: heatmap scale or color is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "heatmap" && (panel.HeatmapBuckets != nil || panel.HeatmapScale != "" || panel.HeatmapColor != "") {
		return fmt.Errorf("%w: heatmap options are only available on heatmap panels", ErrInvalidDashboard)
	}
	return nil
}
