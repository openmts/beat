package model

import "fmt"

// DashboardAxisScaleLinear / DashboardAxisScaleLog 是 Grafana 的 Axis → Scale：
// 缺省线性，"log" 表示以 10 为底的对数刻度（Grafana 的 Logarithmic (base 10)）。
const (
	DashboardAxisScaleLinear = "linear"
	DashboardAxisScaleLog    = "log"
)

// validateDashboardAxisScale 只接受 linear 与 log：对数轴只对折线与柱状面板生效，
// 且声明的下限必须大于 0，因为 0 与负数在对数轴上没有位置。
func validateDashboardAxisScale(panel DashboardPanel) error {
	if panel.Scale == "" {
		return nil
	}
	if panel.Scale != DashboardAxisScaleLinear && panel.Scale != DashboardAxisScaleLog {
		return fmt.Errorf("%w: axis scale must be linear or log", ErrInvalidDashboard)
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: axis scale is only available on time series and bar chart panels", ErrInvalidDashboard)
	}
	if panel.Scale != DashboardAxisScaleLog {
		return nil
	}
	if panel.Min != nil && *panel.Min <= 0 || panel.SoftMin != nil && *panel.SoftMin <= 0 {
		return fmt.Errorf("%w: logarithmic axis needs a minimum above zero", ErrInvalidDashboard)
	}
	return nil
}
