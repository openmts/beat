package model

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxDashboardPanelLinks 限制单个面板的数据链接条数。链接是钻取入口，过多会把面板头
	// 撑成导航栏，小尺寸面板也放不下，因此与 Grafana 一样保持在个位数。
	MaxDashboardPanelLinks = 5
	MaxDashboardLinks      = 8
	// MaxDashboardLinks 是看板级导航链接的条数上限：它渲染在看板头部，超过这个数量会把
	// 标题挤出行外。MaxDashboardLinkTitleBytes 是按钮文案长度；MaxDashboardLinkURLBytes
	// 放宽到 1 KiB，因为 URL 允许携带变量占位符与查询串。
	MaxDashboardLinkTitleBytes = 40
	MaxDashboardLinkURLBytes   = 1024
)

// DashboardLinkIcons 是链接按钮可以选择的图标集合：空值等于默认的 external。
// 只接受固定枚举，作者无法借图标字段注入任意名称或样式。
var DashboardLinkIcons = []string{"external", "dashboard", "alert", "docs", "info", "link"}

func validDashboardLinkIcon(value string) bool {
	return value == "" || slices.Contains(DashboardLinkIcons, value)
}

// DashboardPanelLink 对齐 Grafana 的面板数据链接：Title 是按钮文案，URL 支持 ${...}
// 占位符，NewTab 决定是否在新标签页打开。URL 只接受绝对 http(s) 与站内相对路径，
// 避免把 javascript:/data: 这类可执行目标存进看板。
type DashboardPanelLink struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	NewTab bool   `json:"new_tab,omitempty"`
	// Icon 是按钮左侧的图标名，空值等于 external。渲染器只认 DashboardLinkIcons。
	Icon string `json:"icon,omitempty"`
}

// validateDashboardPanelLinks 校验条数、标题与 URL 形态。行面板不渲染面板头，因此
// 不接受链接；该约束由 rowCarriesPanelOptions 兜住，这里只校验普通面板。
func validateDashboardPanelLinks(panel DashboardPanel) error {
	if len(panel.Links) > MaxDashboardPanelLinks {
		return fmt.Errorf("%w: too many panel data links", ErrInvalidDashboard)
	}
	for _, link := range panel.Links {
		if !dashboardText(link.Title, MaxDashboardLinkTitleBytes) || !validDashboardLinkURL(link.URL) ||
			!validDashboardLinkIcon(link.Icon) {
			return fmt.Errorf("%w: panel data link title, URL or icon is invalid", ErrInvalidDashboard)
		}
	}
	return nil
}

// validDashboardLinkURL 只接受绝对 http(s) 与以单个斜杠开头的站内路径；空白与控制字符
// 一律拒绝。变量占位符原样保留，由前端按当前面板上下文替换。
func validDashboardLinkURL(value string) bool {
	if len(value) > MaxDashboardLinkURLBytes || !utf8.ValidString(value) ||
		strings.IndexFunc(value, unicode.IsControl) != -1 || strings.ContainsRune(value, ' ') {
		return false
	}
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
}

// validateDashboardLinks 校验看板级导航链接：与面板链接同一套标题/URL 规则，只放宽条数。
func validateDashboardLinks(links []DashboardPanelLink) error {
	if len(links) > MaxDashboardLinks {
		return fmt.Errorf("%w: too many dashboard links", ErrInvalidDashboard)
	}
	for _, link := range links {
		if !dashboardText(link.Title, MaxDashboardLinkTitleBytes) || !validDashboardLinkURL(link.URL) ||
			!validDashboardLinkIcon(link.Icon) {
			return fmt.Errorf("%w: dashboard link title, URL or icon is invalid", ErrInvalidDashboard)
		}
	}
	return nil
}
