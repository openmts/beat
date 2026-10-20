package model

import (
	"fmt"
	"slices"
)

// 面板值计算（Grafana 的 Value options → Calculation）：渲染前把一条序列的采样序列压成一个数值。
// 只有「一条序列显示成一个数」的面板类型需要它：折线保留采样点、表格保留原始行，
// 因此这两种类型显式声明时会被拒绝，而不是保存一个渲染器会忽略的字段。
const (
	CalculationLast  = "last"
	CalculationFirst = "first"
	CalculationMin   = "min"
	CalculationMax   = "max"
	CalculationMean  = "mean"
	CalculationSum   = "sum"
	CalculationCount = "count"
	// CalculationRange 是窗口内的极差（最大样本减最小样本），对齐 Grafana 的 Range。
	CalculationRange = "range"
	// CalculationDelta 是窗口内的累计变化量（最新样本减最早样本），对齐 Grafana 的 Delta。
	CalculationDelta = "delta"
	// CalculationPercentChange 是窗口内的百分比变化（(最新-最早)/|最早|×100），最早样本为 0 或
	// 有效样本不足两个时没有可比的基准。公式与服务端常量注释、前端 workspace-calculation 保持一致。
	CalculationPercentChange = "percent_change"
)

var (
	dashboardCalculations = []string{
		CalculationLast, CalculationFirst, CalculationMin, CalculationMax, CalculationMean, CalculationSum,
		CalculationCount, CalculationRange, CalculationDelta, CalculationPercentChange,
	}
	dashboardCalculationTypes = []string{"stat", "gauge", "bargauge", "piechart"}
)

// validateDashboardCalculation 校验值计算方式。空值表示作者没有声明，沿用历史行为
// （取最新有效样本），因此既有看板仍然合法；显式声明只允许 stat/gauge/bargauge/piechart。
func validateDashboardCalculation(panel DashboardPanel) error {
	if panel.Calculation == "" {
		return nil
	}
	if !slices.Contains(dashboardCalculations, panel.Calculation) {
		return fmt.Errorf("%w: panel calculation is invalid", ErrInvalidDashboard)
	}
	if !slices.Contains(dashboardCalculationTypes, panel.Type) {
		return fmt.Errorf("%w: panel calculation is only available on stat, gauge, bar gauge and pie chart panels", ErrInvalidDashboard)
	}
	return nil
}
