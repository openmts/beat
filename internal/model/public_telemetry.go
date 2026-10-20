package model

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// 公共交互遥测只接受固定枚举；任何自由文本、地址、账号或稳定访客标识都不可进入。
const (
	PublicInteractionPageView      = "page_view"
	PublicInteractionNodeOpen      = "node_open"
	PublicInteractionRangeChange   = "range_change"
	PublicInteractionWallboardOpen = "wallboard_open"
)

// 允许上报的粗粒度页面种类。
const (
	PublicSurfaceDashboard = "dashboard"
	PublicSurfaceNode      = "node"
	PublicSurfaceWallboard = "wallboard"
	PublicSurfaceSettings  = "settings"
)

// 拒绝计数使用固定原因集合，避免把校验细节或请求内容写进指标标签。
const (
	PublicRejectionDisabled     = "disabled"
	PublicRejectionBody         = "body"
	PublicRejectionContentType  = "content_type"
	PublicRejectionEvent        = "event"
	PublicRejectionSurface      = "surface"
	PublicRejectionRateLimited  = "rate_limited"
	PublicRejectionUnauthorized = "origin"
)

// 匿名聚合遥测只写入这两个 MTS 计数测量；SQLite 永远只保存策略。
const (
	PublicInteractionTotalMetric    = "public_interaction_total"
	PublicInteractionRejectedMetric = "public_interaction_rejected_total"

	PublicInteractionSampleField = "value"
	PublicInteractionTagEvent    = "event"
	PublicInteractionTagSurface  = "surface"
	PublicInteractionTagReason   = "reason"
)

// 采样上限与保留天数的硬边界。
const (
	MinimumPublicTelemetrySampleCeiling = 1
	MaximumPublicTelemetrySampleCeiling = 100000
	MinimumPublicTelemetryRetentionDays = 1
	MaximumPublicTelemetryRetentionDays = 3650

	MaximumPublicInteractionBytes = 512
	MaximumPublicTelemetryReason  = 32
)

// MaximumPublicInteractionRequestsPerMinute 是单一来源每分钟可接受的交互次数上限。
const MaximumPublicInteractionRequestsPerMinute = 60

var (
	ErrPublicInteractionInvalid = errors.New("invalid public interaction")
	ErrPublicTelemetryPolicy    = errors.New("invalid public telemetry policy")
)

var publicInteractionEvents = []string{
	PublicInteractionPageView, PublicInteractionNodeOpen, PublicInteractionRangeChange, PublicInteractionWallboardOpen,
}

var publicRejectionReasons = []string{
	PublicRejectionDisabled, PublicRejectionBody, PublicRejectionContentType, PublicRejectionEvent,
	PublicRejectionSurface, PublicRejectionRateLimited, PublicRejectionUnauthorized,
}

var publicInteractionSurfaces = []string{
	PublicSurfaceDashboard, PublicSurfaceNode, PublicSurfaceWallboard, PublicSurfaceSettings,
}

// PublicRejectionReasons 返回固定的拒绝原因枚举顺序。
func PublicRejectionReasons() []string { return slices.Clone(publicRejectionReasons) }

// ValidPublicRejectionReason 判断原因是否为已登记的拒绝枚举。
func ValidPublicRejectionReason(reason string) bool {
	return slices.Contains(publicRejectionReasons, reason)
}

// PublicInteractionEvents 返回固定的事件枚举顺序。
func PublicInteractionEvents() []string { return slices.Clone(publicInteractionEvents) }

// PublicInteractionSurfaces 返回固定的页面种类枚举顺序。
func PublicInteractionSurfaces() []string { return slices.Clone(publicInteractionSurfaces) }

// ValidPublicInteractionEvent 判断事件是否为已登记枚举。
func ValidPublicInteractionEvent(event string) bool {
	return slices.Contains(publicInteractionEvents, event)
}

// ValidPublicInteractionSurface 判断页面种类是否为已登记枚举。
func ValidPublicInteractionSurface(surface string) bool {
	return slices.Contains(publicInteractionSurfaces, surface)
}

// PublicInteraction 是一次已经被接受的上报；只有两个固定枚举字段。
type PublicInteraction struct {
	Event   string `json:"event"`
	Surface string `json:"surface"`
}

// Validate 只接受已登记的枚举取值。
func (interaction PublicInteraction) Validate() error {
	if !ValidPublicInteractionEvent(interaction.Event) || !ValidPublicInteractionSurface(interaction.Surface) {
		return ErrPublicInteractionInvalid
	}
	return nil
}

// PublicTelemetryPolicy 是有界的匿名聚合遥测策略；SQLite 只保存策略，不保存样本。
type PublicTelemetryPolicy struct {
	Enabled       bool      `json:"enabled"`
	AllowedEvents []string  `json:"allowed_events"`
	SampleCeiling int       `json:"sample_ceiling"`
	RetentionDays int       `json:"retention_days"`
	Revision      int       `json:"revision"`
	UpdatedBy     string    `json:"updated_by"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DefaultPublicTelemetryPolicy 默认关闭；只有 owner 显式开启后才接受匿名计数。
func DefaultPublicTelemetryPolicy() PublicTelemetryPolicy {
	return PublicTelemetryPolicy{
		Enabled: false, AllowedEvents: PublicInteractionEvents(), SampleCeiling: 1000,
		RetentionDays: 30, Revision: 1,
	}
}

// Normalize 去重并排序事件集合，并收敛空值。
func (policy *PublicTelemetryPolicy) Normalize() {
	allowed := make([]string, 0, len(policy.AllowedEvents))
	for _, event := range policy.AllowedEvents {
		trimmed := strings.TrimSpace(event)
		if trimmed == "" || slices.Contains(allowed, trimmed) {
			continue
		}
		allowed = append(allowed, trimmed)
	}
	slices.Sort(allowed)
	policy.AllowedEvents = allowed
	if policy.SampleCeiling == 0 {
		policy.SampleCeiling = MinimumPublicTelemetrySampleCeiling
	}
	if policy.RetentionDays == 0 {
		policy.RetentionDays = MinimumPublicTelemetryRetentionDays
	}
	policy.UpdatedBy = strings.TrimSpace(policy.UpdatedBy)
}

// Validate 拒绝未登记事件、越界采样上限与越界保留期。
func (policy PublicTelemetryPolicy) Validate() error {
	if len(policy.AllowedEvents) == 0 {
		return ErrPublicTelemetryPolicy
	}
	for _, event := range policy.AllowedEvents {
		if !slices.Contains(publicInteractionEvents, event) {
			return ErrPublicTelemetryPolicy
		}
	}
	if policy.SampleCeiling < MinimumPublicTelemetrySampleCeiling || policy.SampleCeiling > MaximumPublicTelemetrySampleCeiling {
		return ErrPublicTelemetryPolicy
	}
	if policy.RetentionDays < MinimumPublicTelemetryRetentionDays || policy.RetentionDays > MaximumPublicTelemetryRetentionDays {
		return ErrPublicTelemetryPolicy
	}
	if policy.Revision < 1 || len(policy.UpdatedBy) > 64 {
		return ErrPublicTelemetryPolicy
	}
	return nil
}

// IsPublicInteractionMetric 判断键是否为已登记的匿名聚合遥测测量。
func IsPublicInteractionMetric(key string) bool {
	return key == PublicInteractionTotalMetric || key == PublicInteractionRejectedMetric
}

// PublicInteractionMetrics 返回全部已登记的匿名聚合遥测测量名称。
func PublicInteractionMetrics() []string {
	return []string{PublicInteractionTotalMetric, PublicInteractionRejectedMetric}
}

// Allows 报告某个事件当前是否被策略允许上报。
func (policy PublicTelemetryPolicy) Allows(event string) bool {
	return policy.Enabled && slices.Contains(policy.AllowedEvents, event)
}
