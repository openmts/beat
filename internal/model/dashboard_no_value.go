package model

import (
	"fmt"
	"slices"
)

// MaxPanelNoValueBytes 是占位文本的字节上限。它是 Grafana 的 Standard options → No value：
// 某个字段没有样本时代替渲染器默认破折号的一行短文本。
const MaxPanelNoValueBytes = 32

// dashboardNoValueTypes 列出会显示占位文本的面板：取值面板、表格与状态时间线都会为缺失样本
// 渲染一行文本，折线、柱状图与热力图没有这种格子，带上它只会保存出一份渲染器忽略的配置。
var dashboardNoValueTypes = []string{"stat", "gauge", "bargauge", "table", "statetimeline", "piechart"}

// validateDashboardNoValue 校验面板级占位文本：可选字段，空值等于沿用默认破折号。
func validateDashboardNoValue(panel DashboardPanel) error {
	if panel.NoValue == "" {
		return nil
	}
	if !slices.Contains(dashboardNoValueTypes, panel.Type) {
		return fmt.Errorf("%w: panel no value text is not available on this panel type", ErrInvalidDashboard)
	}
	if !dashboardText(panel.NoValue, MaxPanelNoValueBytes) {
		return fmt.Errorf("%w: panel no value text must be printable and at most %d bytes", ErrInvalidDashboard, MaxPanelNoValueBytes)
	}
	return nil
}
