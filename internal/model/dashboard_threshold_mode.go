package model

import (
	"fmt"
	"slices"
)

// 阈值显示模式（Grafana 面板 Thresholds 的颜色模式）：控制阈值如何出现在面板上。
const (
	// ThresholdModeOff 保留阈值数值但关闭阈值取色与阈值线，stat/gauge 回到系列颜色。
	ThresholdModeOff = "off"
	// ThresholdModeLine 是缺省行为：只画阈值虚线（既有看板不带该字段时行为不变）。
	ThresholdModeLine = "line"
	// ThresholdModeArea 在相邻阈值之间画半透明色带。
	ThresholdModeArea = "area"
	// ThresholdModeBackground 按数值命中的最高阈值给绘图区铺一层淡色背景。
	ThresholdModeBackground = "background"
)

var dashboardThresholdModes = []string{ThresholdModeOff, ThresholdModeLine, ThresholdModeArea, ThresholdModeBackground}

// validateDashboardThresholdMode 校验阈值显示模式。空值表示作者没有声明，
// 沿用历史行为（timeseries 画虚线、stat/gauge 按阈值取色），因此不参与面板类型约束；
// 显式声明时只有 timeseries 能画线、色带与背景，其他面板允许 off 来关闭阈值取色。
func validateDashboardThresholdMode(panel DashboardPanel) error {
	mode := panel.ThresholdMode
	if mode == "" {
		return nil
	}
	if !slices.Contains(dashboardThresholdModes, mode) {
		return fmt.Errorf("%w: threshold display mode is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "timeseries" && mode != ThresholdModeOff {
		return fmt.Errorf("%w: threshold display mode is only available on timeseries panels", ErrInvalidDashboard)
	}
	return nil
}
