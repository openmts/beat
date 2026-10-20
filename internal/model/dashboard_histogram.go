package model

import (
	"fmt"
	"slices"
)

// 直方图沿用热力图的取值轴命名：分桶数量、线性/对数刻度与基础配色，另外增加「合并序列」。
var dashboardHistogramScales = []string{"linear", "log"}

const (
	// MinDashboardHistogramBuckets / MaxDashboardHistogramBuckets 限制取值轴分桶数量：
	// 分桶过少看不出分布形状，分桶过多会把每个柱子压成细线并放大渲染成本。
	MinDashboardHistogramBuckets = 2
	MaxDashboardHistogramBuckets = 64
	// dashboardHistogramDefaultBuckets 与 Grafana 直方图的缺省分桶数量一致。
	dashboardHistogramDefaultBuckets = 12
	dashboardHistogramDefaultScale   = "linear"
	dashboardHistogramDefaultColor   = "blue"
	// dashboardHistogramDefaultMerge 与 Grafana 的 Combine series 缺省一致：每个序列各画一组柱子，
	// 打开后所有序列合并成同一个分布。
	dashboardHistogramDefaultMerge = false
)

// DefaultDashboardHistogramBuckets 返回缺省分桶数量，供文档与前端保持一致。
func DefaultDashboardHistogramBuckets() int { return dashboardHistogramDefaultBuckets }

// DefaultDashboardHistogramScale 返回缺省计数轴刻度。
func DefaultDashboardHistogramScale() string { return dashboardHistogramDefaultScale }

// DefaultDashboardHistogramColor 返回缺省配色；它与其它面板的系列默认色一致。
func DefaultDashboardHistogramColor() string { return dashboardHistogramDefaultColor }

// DefaultDashboardHistogramMerge 返回缺省合并行为。
func DefaultDashboardHistogramMerge() bool { return dashboardHistogramDefaultMerge }

// validateDashboardHistogram 校验分桶数量、计数轴刻度、配色与合并开关；这四项只对 histogram 生效，
// 其它面板类型带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardHistogram(panel DashboardPanel) error {
	buckets := dashboardHistogramDefaultBuckets
	if panel.HistogramBuckets != nil {
		buckets = *panel.HistogramBuckets
	}
	scale := panel.HistogramScale
	if scale == "" {
		scale = dashboardHistogramDefaultScale
	}
	color := panel.HistogramColor
	if color == "" {
		color = dashboardHistogramDefaultColor
	}
	if buckets < MinDashboardHistogramBuckets || buckets > MaxDashboardHistogramBuckets {
		return fmt.Errorf("%w: histogram bucket count must be between %d and %d", ErrInvalidDashboard, MinDashboardHistogramBuckets, MaxDashboardHistogramBuckets)
	}
	if !slices.Contains(dashboardHistogramScales, scale) || !dashboardColor(color) {
		return fmt.Errorf("%w: histogram scale or color is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "histogram" && (panel.HistogramBuckets != nil || panel.HistogramScale != "" || panel.HistogramColor != "" || panel.HistogramMerge != nil) {
		return fmt.Errorf("%w: histogram options are only available on histogram panels", ErrInvalidDashboard)
	}
	return nil
}
