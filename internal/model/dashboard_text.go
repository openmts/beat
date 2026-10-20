package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDashboardTextBytes 限制文本面板内容：它是给人看的短文档，不是页面正文；4 KiB 足够写下
// 面板用途、阈值口径与排障入口，也能避免看板定义被单块文本顶到 64 KiB 的总上限。
const MaxDashboardTextBytes = 4096

// validateDashboardTextPanel 校验文本面板：它像行面板一样不查询数据，因此不允许任何查询或
// 可视化字段（标题、尺寸、Markdown 内容与数据链接除外）；反过来，非文本面板带上 text
// 一律拒绝，避免保存出永远不会渲染的配置。
func validateDashboardTextPanel(panel DashboardPanel) error {
	if panel.Type != "text" {
		if panel.Text != "" {
			return fmt.Errorf("%w: text content is only available on text panels", ErrInvalidDashboard)
		}
		return nil
	}
	if panelCarriesQueryOptions(panel) {
		return fmt.Errorf("%w: text panels only carry a title, size, markdown and links", ErrInvalidDashboard)
	}
	if !validDashboardText(panel.Text) {
		return fmt.Errorf("%w: text panel content is invalid", ErrInvalidDashboard)
	}
	return nil
}

// validDashboardText 允许空内容（先建面板再补文档），要求 UTF-8 合法、长度有界，
// 且除换行、回车与制表符外不含控制字符（粘贴的 CRLF 文本同样要能保存）。
func validDashboardText(value string) bool {
	if len(value) > MaxDashboardTextBytes || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t'
	}) == -1
}
