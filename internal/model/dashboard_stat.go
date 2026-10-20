package model

import (
	"fmt"
	"slices"
)

// 值面板外观沿用 Grafana Stat 的命名：Color mode 决定阈值色落在数值还是面板底色上，
// Text mode 决定主文本与附属说明显示什么（auto/value/name/none），Text size 决定主文本字号档位，
// Orientation 决定名称与数值的排版方向。
var (
	dashboardStatColorModes   = []string{"value", "background", "none"}
	dashboardStatTextModes    = []string{"auto", "value", "name", "none"}
	dashboardStatTextSizes    = []string{"auto", "sm", "md", "lg", "xl"}
	dashboardStatOrientations = []string{"auto", "horizontal", "vertical"}
)

// validateDashboardStatOptions 校验值面板外观。字段都是可选的，空值等于 Grafana 的缺省
// （value / auto），因此历史看板不受影响；它们只对 stat 面板有意义，其它面板类型带上会被拒绝，
// 避免保存出渲染器永远忽略的配置。
func validateDashboardStatOptions(panel DashboardPanel) error {
	if panel.StatColorMode == "" && panel.StatTextMode == "" && panel.StatTextSize == "" &&
		panel.StatOrientation == "" {
		return nil
	}
	if panel.Type != "stat" {
		return fmt.Errorf("%w: stat styles are only available on stat panels", ErrInvalidDashboard)
	}
	if panel.StatColorMode != "" && !slices.Contains(dashboardStatColorModes, panel.StatColorMode) {
		return fmt.Errorf("%w: stat color mode is invalid", ErrInvalidDashboard)
	}
	if panel.StatTextMode != "" && !slices.Contains(dashboardStatTextModes, panel.StatTextMode) {
		return fmt.Errorf("%w: stat text mode is invalid", ErrInvalidDashboard)
	}
	if panel.StatTextSize != "" && !slices.Contains(dashboardStatTextSizes, panel.StatTextSize) {
		return fmt.Errorf("%w: stat text size is invalid", ErrInvalidDashboard)
	}
	if panel.StatOrientation != "" && !slices.Contains(dashboardStatOrientations, panel.StatOrientation) {
		return fmt.Errorf("%w: stat orientation is invalid", ErrInvalidDashboard)
	}
	return nil
}
