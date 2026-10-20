package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxPanelDescriptionBytes 是面板描述的字节上限。描述只出现在面板头的信息图标 tooltip 里，
// 因此明显小于看板级描述（2048 字节），但仍给 CJK 留出足够长度。
const MaxPanelDescriptionBytes = 512

// validateDashboardPanelDescription 校验面板描述：可选字段，空值等于没有描述。
// 与服务端既有的文本口径一致：必须是合法 UTF-8、不含 NUL，且不超过字节上限。
func validateDashboardPanelDescription(panel DashboardPanel) error {
	if panel.Description == "" {
		return nil
	}
	if len(panel.Description) > MaxPanelDescriptionBytes || !utf8.ValidString(panel.Description) ||
		strings.ContainsRune(panel.Description, 0) {
		return fmt.Errorf("%w: panel description is invalid", ErrInvalidDashboard)
	}
	return nil
}
