package model

import (
	"fmt"
	"unicode/utf8"
)

// MaxDashboardAxisLabelBytes 是 Y 轴标题的字节上限。标题只是坐标轴旁的一行短文本，
// 40 字节足够写清口径，同时避免把绘图区挤窄。
const MaxDashboardAxisLabelBytes = 40

// validateDashboardAxisLabel 校验 Y 轴标题：可选字段，空值等于不显示标题。
// 只对折线与柱状面板生效，文本口径与服务端其它描述字段一致（合法 UTF-8、不含 NUL、有界）。
func validateDashboardAxisLabel(panel DashboardPanel) error {
	if panel.AxisLabel == "" {
		return nil
	}
	if panel.Type != "timeseries" && panel.Type != "barchart" {
		return fmt.Errorf("%w: axis label is only available on time series and bar chart panels", ErrInvalidDashboard)
	}
	if len(panel.AxisLabel) > MaxDashboardAxisLabelBytes || !utf8.ValidString(panel.AxisLabel) {
		return fmt.Errorf("%w: axis label is invalid", ErrInvalidDashboard)
	}
	// 标题只有一行：换行、制表符与其它控制字符都不出现在坐标轴旁，与前端 validAxisLabel 同口径。
	for _, character := range panel.AxisLabel {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%w: axis label is invalid", ErrInvalidDashboard)
		}
	}
	return nil
}
