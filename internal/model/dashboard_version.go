package model

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxDashboardVersions 是每块看板保留的历史版本数（含当前版本）。版本历史用于回看与恢复，
	// 不是无限存档：超出后按版本号从旧到新裁剪，避免看板定义无限堆积。
	MaxDashboardVersions = 20
	// MaxDashboardVersionSummaryBytes 限制单条版本备注的字节数。
	MaxDashboardVersionSummaryBytes = 200
)

// MetricDashboardVersion 是看板某个历史版本的元数据。定义体最大 64 KiB，列表接口不返回它，
// 需要精确内容时按版本号单独读取。
type MetricDashboardVersion struct {
	DashboardID string    `json:"dashboard_id"`
	Revision    int       `json:"revision"`
	Title       string    `json:"title,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
}

// ValidateDashboardVersionSummary 校验版本备注：允许留空，最多 200 字节且不含控制字符。
func ValidateDashboardVersionSummary(value string) error {
	if value == "" {
		return nil
	}
	if !utf8.ValidString(value) || len(value) > MaxDashboardVersionSummaryBytes ||
		strings.IndexFunc(value, unicode.IsControl) != -1 {
		return fmt.Errorf("%w: version summary is invalid", ErrInvalidDashboard)
	}
	return nil
}
