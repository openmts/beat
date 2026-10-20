package model

import "fmt"

// DashboardAxisPlacementLeft / DashboardAxisPlacementRight 是 Grafana 的 Axis → Placement：
// 留空等于 auto（缺省画在绘图区左侧），显式给出时才记录。
const (
	DashboardAxisPlacementLeft  = "left"
	DashboardAxisPlacementRight = "right"
)

// validateDashboardAxisDisplay 校验 Grafana 的显示类轴选项：
// Grid 是 Show grid lines（false 关掉水平网格线，留空等于 true），
// AxisPlacement 是 Placement（left/right，留空等于 auto），
// CenteredZero 是 Centered zero（坐标轴对称展开，与固定上下限互斥）。三者都只对折线与柱状面板生效。
func validateDashboardAxisDisplay(panel DashboardPanel) error {
	if panel.Grid == nil && panel.AxisPlacement == "" && !panel.CenteredZero {
		return nil
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: axis display options are only available on time series and bar chart panels", ErrInvalidDashboard)
	}
	if panel.AxisPlacement != "" && panel.AxisPlacement != DashboardAxisPlacementLeft && panel.AxisPlacement != DashboardAxisPlacementRight {
		return fmt.Errorf("%w: axis placement must be left or right", ErrInvalidDashboard)
	}
	// 固定上下限会直接决定坐标轴，居中就没有意义，因此两者互斥。
	if panel.CenteredZero && (panel.Min != nil || panel.Max != nil) {
		return fmt.Errorf("%w: centered zero needs an automatic axis without fixed limits", ErrInvalidDashboard)
	}
	return nil
}
