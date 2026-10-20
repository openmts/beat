package model

import (
	"fmt"
	"slices"
)

// DashboardCellDisplayModes 是表格面板「单元格显示模式」的固定枚举（Grafana 的 Cell display mode）。
// text 保持历史行为——只有值映射能改文本颜色；color_text 用阈值给文本上色；color_background 给
// 单元格上色；gradient 在当前阈值区间内插值背景透明度。缺省等于 text。
var DashboardCellDisplayModes = []string{"text", "color_text", "color_background", "gradient"}

// DashboardTableOrientations 是表格面板「形态」的固定枚举（Grafana 的 Table 布局）：long 是历史
// 默认——一行一个采样；wide 是 Grafana 默认的宽表——一列一条序列、一行一个时间点。缺省等于 long。
var DashboardTableOrientations = []string{"long", "wide"}

// validateDashboardTable 校验表格专属选项。两种选项都只对 table 面板有效：其它类型带上它渲染不出
// 效果，属于「配置写了但永远不生效」的看板，因此直接拒绝而不是静默忽略。
func validateDashboardTable(panel DashboardPanel) error {
	if err := validateDashboardTableChoice(panel, panel.CellDisplayMode, DashboardCellDisplayModes, "cell display mode"); err != nil {
		return err
	}
	return validateDashboardTableChoice(panel, panel.TableOrientation, DashboardTableOrientations, "table orientation")
}

// validateDashboardTableChoice 共用「仅 table 面板 + 固定枚举」这两条规则：两个表格选项的区别只有
// 字段名与枚举，共用一个助手避免两份会各自漂移的判定。
func validateDashboardTableChoice(panel DashboardPanel, value *string, allowed []string, label string) error {
	if value == nil {
		return nil
	}
	if panel.Type != "table" {
		return fmt.Errorf("%w: %s is only available on table panels", ErrInvalidDashboard, label)
	}
	if !slices.Contains(allowed, *value) {
		return fmt.Errorf("%w: %s is invalid", ErrInvalidDashboard, label)
	}
	return nil
}
