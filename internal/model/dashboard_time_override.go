package model

import (
	"fmt"
	"regexp"
)

// 面板级时间覆盖沿用 Grafana 的相对时间写法：`now` 或 `now-<数值><单位>`，
// 单位是 s/m/h/d。它是 v8 看板 JSON 里的可选字段，不需要新的迁移号。
var dashboardTimeTokenPattern = regexp.MustCompile(`^now(-[0-9]{1,4}(s|m|h|d))?$`)

var dashboardTimeUnitSeconds = map[byte]int{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}

// 面板级时间平移沿用 Grafana 的时长写法，例如 `1d`、`12h`：窗口整体后移，用于对比上一周期。
var dashboardShiftPattern = regexp.MustCompile(`^[0-9]{1,4}(s|m|h|d)$`)

// dashboardTimeTokenSeconds 把语法合法的 token 解析成相对现在的秒数。
func dashboardTimeTokenSeconds(token string) (int, bool) {
	if !dashboardTimeTokenPattern.MatchString(token) {
		return 0, false
	}
	if token == "now" {
		return 0, true
	}
	suffix := token[len("now-"):]
	value := 0
	for _, digit := range suffix[:len(suffix)-1] {
		value = value*10 + int(digit-'0')
	}
	return value * dashboardTimeUnitSeconds[suffix[len(suffix)-1]], true
}

// validateDashboardTimeOverride 允许整段留空（跟随看板范围）；只给起点时终点回落到 now。
func validateDashboardTimeOverride(panel DashboardPanel) error {
	if panel.TimeTo != "" && panel.TimeFrom == "" {
		return fmt.Errorf("%w: a panel time override needs a start", ErrInvalidDashboard)
	}
	if panel.TimeFrom == "" {
		return nil
	}
	start, ok := dashboardTimeTokenSeconds(panel.TimeFrom)
	if !ok {
		return fmt.Errorf("%w: panel time override start is invalid", ErrInvalidDashboard)
	}
	end := 0
	if panel.TimeTo != "" {
		if end, ok = dashboardTimeTokenSeconds(panel.TimeTo); !ok {
			return fmt.Errorf("%w: panel time override end is invalid", ErrInvalidDashboard)
		}
	}
	if window := start - end; window < 60 || window > MaxDashboardRangeSeconds {
		return fmt.Errorf("%w: panel time override window must be 1 minute to 31 days", ErrInvalidDashboard)
	}
	return nil
}

// validateDashboardTimeShift 允许留空（不平移）；非空时必须是 1 秒到 31 天之间的时长。
func validateDashboardTimeShift(panel DashboardPanel) error {
	if panel.TimeShift == "" {
		return nil
	}
	if !dashboardShiftPattern.MatchString(panel.TimeShift) {
		return fmt.Errorf("%w: panel time shift is invalid", ErrInvalidDashboard)
	}
	seconds, ok := dashboardTimeTokenSeconds("now-" + panel.TimeShift)
	if !ok || seconds < 1 || seconds > MaxDashboardRangeSeconds {
		return fmt.Errorf("%w: panel time shift must be 1 second to 31 days", ErrInvalidDashboard)
	}
	return nil
}
