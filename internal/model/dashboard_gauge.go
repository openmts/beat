package model

import "fmt"

// validateDashboardGaugeThresholds 校验 Grafana Gauge 的阈值刻度与标签。两个字段都是可选的，
// nil 分别等于「显示刻度」与「不显示标签」，因此历史看板不受影响；它们只对 gauge 面板有意义，
// 其它面板类型带上会被拒绝，避免保存出渲染器永远忽略的配置。
func validateDashboardGaugeThresholds(panel DashboardPanel) error {
	if panel.GaugeThresholdMarkers == nil && panel.GaugeThresholdLabels == nil {
		return nil
	}
	if panel.Type != "gauge" {
		return fmt.Errorf("%w: gauge threshold markers are only available on gauge panels", ErrInvalidDashboard)
	}
	// 标签依附刻度：刻度被显式关闭时标签没有落脚点。
	if panel.GaugeThresholdLabels != nil && *panel.GaugeThresholdLabels &&
		panel.GaugeThresholdMarkers != nil && !*panel.GaugeThresholdMarkers {
		return fmt.Errorf("%w: gauge threshold labels need threshold markers", ErrInvalidDashboard)
	}
	// 没有阈值时标签无从显示，保存出这种配置只会让作者困惑。
	if panel.GaugeThresholdLabels != nil && *panel.GaugeThresholdLabels && len(panel.Thresholds) == 0 {
		return fmt.Errorf("%w: gauge threshold labels need thresholds", ErrInvalidDashboard)
	}
	return nil
}
