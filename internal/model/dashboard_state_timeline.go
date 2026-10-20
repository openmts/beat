package model

import (
	"fmt"
	"slices"
)

// 状态时间线沿用 Grafana 的取值标签模式：auto 只在色块放得下时显示取值，always 总是显示，
// never 只在提示里给出取值。
var dashboardStateTimelineValueModes = []string{"auto", "always", "never"}

const (
	// dashboardStateTimelineDefaultMerge 与 Grafana 一致：相邻的相同状态默认合并成一个色块，
	// 否则每个采样点都会变成一条独立竖线，时间线会被切成无法阅读的栅格。
	dashboardStateTimelineDefaultMerge = true
	dashboardStateTimelineDefaultValue = "auto"
)

// DefaultDashboardStateTimelineMerge 返回缺省合并行为，供文档与前端保持一致。
func DefaultDashboardStateTimelineMerge() bool { return dashboardStateTimelineDefaultMerge }

// DefaultDashboardStateTimelineShowValues 返回缺省取值标签模式。
func DefaultDashboardStateTimelineShowValues() string { return dashboardStateTimelineDefaultValue }

// validateDashboardStateTimeline 校验合并开关与取值标签模式；这两项只对 statetimeline 生效，
// 其它面板类型带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardStateTimeline(panel DashboardPanel) error {
	mode := panel.TimelineShowValues
	if mode == "" {
		mode = dashboardStateTimelineDefaultValue
	}
	if !slices.Contains(dashboardStateTimelineValueModes, mode) {
		return fmt.Errorf("%w: state timeline value label mode is invalid", ErrInvalidDashboard)
	}
	if panel.Type != "statetimeline" && (panel.TimelineMerge != nil || panel.TimelineShowValues != "") {
		return fmt.Errorf("%w: state timeline options are only available on state timeline panels", ErrInvalidDashboard)
	}
	return nil
}
