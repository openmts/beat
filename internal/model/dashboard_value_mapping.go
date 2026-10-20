package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxDashboardValueMappings 是单个面板允许的值映射条数。
	MaxDashboardValueMappings = 8
	// MaxDashboardMappingTextBytes 限制映射文本长度：它只用于展示，不能成为无界输入。
	MaxDashboardMappingTextBytes = 32
)

// 值映射沿用 Grafana 的三种匹配方式；特殊值只支持空值——Beat 的查询结果里缺样本就是 null，
// 不会出现 NaN，因此不声明一个永远匹配不到的特殊值。
var (
	dashboardMappingTypes = []string{"value", "range", "special"}
	dashboardMappingMatch = "null"
)

// validateDashboardValueMappings 校验值映射：类型、必填参数、范围顺序、文本与调色板颜色。
// 第一条命中的映射生效，因此这里只要求每条自身合法，不要求区间互不重叠。
func validateDashboardValueMappings(panel DashboardPanel) error {
	if len(panel.ValueMappings) > MaxDashboardValueMappings {
		return fmt.Errorf("%w: at most %d value mappings are allowed", ErrInvalidDashboard, MaxDashboardValueMappings)
	}
	// 状态时间线用值映射给离散状态命名与取色，因此与 stat/gauge/table/bargauge/piechart 同一组；
	// geo map 的标记按阈值取色，不消费值映射文本。
	if len(panel.ValueMappings) > 0 && (panel.Type == "timeseries" || panel.Type == "barchart" ||
		panel.Type == "heatmap" || panel.Type == "histogram" || panel.Type == "geomap") {
		return fmt.Errorf("%w: value mappings are only available on stat, gauge, table, bar gauge, pie chart and state timeline panels", ErrInvalidDashboard)
	}
	for _, mapping := range panel.ValueMappings {
		if err := validateDashboardValueMapping(mapping); err != nil {
			return err
		}
	}
	return nil
}

func validateDashboardValueMapping(mapping DashboardValueMapping) error {
	if !containsString(dashboardMappingTypes, mapping.Type) {
		return fmt.Errorf("%w: value mapping type is invalid", ErrInvalidDashboard)
	}
	if !validDashboardMappingText(mapping.Text) || (mapping.Color != "" && !dashboardColor(mapping.Color)) {
		return fmt.Errorf("%w: value mapping text or color is invalid", ErrInvalidDashboard)
	}
	// 既没有文本也没有颜色时映射不会改变任何显示，直接拒绝而不是保存一条空规则。
	if mapping.Text == "" && mapping.Color == "" {
		return fmt.Errorf("%w: value mapping must set text or color", ErrInvalidDashboard)
	}
	switch mapping.Type {
	case "value":
		if mapping.Value == nil || !isFinite(*mapping.Value) || mapping.Match != "" {
			return fmt.Errorf("%w: value mapping requires a finite value", ErrInvalidDashboard)
		}
	case "range":
		if mapping.Value != nil || mapping.Match != "" || (mapping.From == nil && mapping.To == nil) {
			return fmt.Errorf("%w: range mapping requires a bound", ErrInvalidDashboard)
		}
		if mapping.From != nil && !isFinite(*mapping.From) || mapping.To != nil && !isFinite(*mapping.To) {
			return fmt.Errorf("%w: range bounds must be finite", ErrInvalidDashboard)
		}
		if mapping.From != nil && mapping.To != nil && *mapping.From >= *mapping.To {
			return fmt.Errorf("%w: range start must be below its end", ErrInvalidDashboard)
		}
	case "special":
		if mapping.Value != nil || mapping.From != nil || mapping.To != nil || mapping.Match != dashboardMappingMatch {
			return fmt.Errorf("%w: special mapping must match null", ErrInvalidDashboard)
		}
	}
	return nil
}

// validDashboardMappingText 允许空文本（只改颜色），但拒绝控制字符与超长文本。
func validDashboardMappingText(value string) bool {
	if value == "" {
		return true
	}
	return utf8.ValidString(value) && len(value) <= MaxDashboardMappingTextBytes && strings.IndexFunc(value, unicode.IsControl) == -1
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
