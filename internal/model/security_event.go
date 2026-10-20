package model

import "time"

// 登录安全事件类型与通知 v6 的固定词表保持一致。
const (
	SecurityEventLoginSucceeded  = "security.login_succeeded"
	SecurityEventLoginSuspicious = "security.login_suspicious"
)

const (
	SecuritySeverityInfo   = "info"
	SecuritySeverityMedium = "medium"
	SecuritySeverityHigh   = "high"
)

// 路由状态描述安全事件对应的通知消息去向。
const (
	SecurityRoutingRouted      = "routed"
	SecurityRoutingNotRouted   = "not_routed"
	SecurityRoutingSkipped     = "skipped"
	SecurityRoutingUnavailable = "unavailable"
)

// 来源隐私级别：默认只发送截断前缀，owner 可显式选择完整地址。
const (
	SecurityPrivacyCoarse     = "coarse"
	SecurityPrivacyFullSource = "full_source"
)

const (
	SecuritySourceClassLoopback  = "loopback"
	SecuritySourceClassPrivate   = "private"
	SecuritySourceClassLinkLocal = "link_local"
	SecuritySourceClassPublic    = "public"
	SecuritySourceClassUnknown   = "unknown"
)

// 事件里的认证方法使用比会话更稳定的固定词表。
const (
	SecurityAuthMethodPassword  = "password"
	SecurityAuthMethodOIDC      = "oidc"
	SecurityAuthMethodRecovery  = "recovery"
	SecurityAuthMethodBootstrap = "bootstrap"
	SecurityAuthMethodUnknown   = "unknown"
)

const (
	SecurityDeviceClassDesktop    = "desktop"
	SecurityDeviceClassMobile     = "mobile"
	SecurityDeviceClassTablet     = "tablet"
	SecurityDeviceClassAutomation = "automation"
	SecurityDeviceClassUnknown    = "unknown"
)

const (
	SecuritySourceFamilyIPv4    = "ipv4"
	SecuritySourceFamilyIPv6    = "ipv6"
	SecuritySourceFamilyUnknown = "unknown"
)

// 可疑登录原因代码是固定有界词表，禁止写入自由文本。
const (
	SecurityReasonRateLimitLockout = "rate_limit_lockout"
	SecurityReasonInvalidTOTP      = "repeated_invalid_totp"
	SecurityReasonNewSourcePrefix  = "new_source_prefix"
	SecurityReasonNewDeviceFamily  = "new_device_family"
	SecurityReasonDisabledIdentity = "disabled_or_revoked_identity"
	SecurityReasonDistantSource    = "concurrent_distant_source"
)

var securityReasonCodes = []string{
	SecurityReasonRateLimitLockout, SecurityReasonInvalidTOTP, SecurityReasonNewSourcePrefix,
	SecurityReasonNewDeviceFamily, SecurityReasonDisabledIdentity, SecurityReasonDistantSource,
}

// SecurityReasonCodes 返回原因代码词表副本，供 API 选项与校验复用。
func SecurityReasonCodes() []string {
	return append([]string(nil), securityReasonCodes...)
}

type SecurityEvent struct {
	ID               string                  `json:"id"`
	Kind             string                  `json:"kind"`
	Severity         string                  `json:"severity"`
	ActorUserID      string                  `json:"actor_user_id"`
	ActorDisplayName string                  `json:"actor_display_name"`
	AuthMethod       string                  `json:"auth_method"`
	SourceClass      string                  `json:"source_class"`
	SourceFamily     string                  `json:"source_family"`
	SourcePrefix     string                  `json:"-"`
	SourceSummary    string                  `json:"source_summary"`
	SourceKey        string                  `json:"-"`
	SourceKind       string                  `json:"source_kind"`
	SourceID         string                  `json:"source_id"`
	BrowserFamily    string                  `json:"browser_family"`
	OSFamily         string                  `json:"os_family"`
	DeviceClass      string                  `json:"device_class"`
	DeviceSummary    string                  `json:"device_summary"`
	CountryCode      string                  `json:"country_code"`
	Region           string                  `json:"region"`
	Reasons          []string                `json:"reasons"`
	PolicyRevision   int64                   `json:"policy_revision"`
	RoutingState     string                  `json:"routing_state"`
	AuditEventID     string                  `json:"audit_event_id"`
	MessageID        string                  `json:"message_id"`
	OccurredAt       time.Time               `json:"occurred_at"`
	RetainUntil      time.Time               `json:"retain_until"`
	AcknowledgedAt   *time.Time              `json:"acknowledged_at,omitempty"`
	AcknowledgedBy   string                  `json:"acknowledged_by,omitempty"`
	AcknowledgedNote string                  `json:"acknowledged_note,omitempty"`
	DeliveryState    string                  `json:"delivery_state"`
	DeliveryChannels int                     `json:"delivery_channels"`
	DeliveryFailures int                     `json:"delivery_failures"`
	DeliveryPending  int                     `json:"delivery_pending"`
	Deliveries       []SecurityEventDelivery `json:"deliveries,omitempty"`
}

// SecurityEventDelivery 是事件详情里关联的脱敏投递行。
type SecurityEventDelivery struct {
	DeliveryID   string    `json:"delivery_id"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	State        string    `json:"state"`
	AttemptCount int       `json:"attempt_count"`
	ErrorCode    string    `json:"error_code,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SecurityEventSource 是 owner 显式揭示的受保护来源地址：事件本身只保存截断前缀，
// 这里的规范地址来自同一事务写入的审计记录。
type SecurityEventSource struct {
	EventID    string    `json:"event_id"`
	Address    string    `json:"address"`
	CapturedAt time.Time `json:"captured_at"`
}

type SecurityEventFilter struct {
	Kind          string
	Severity      string
	ActorID       string
	SourceClass   string
	Reason        string
	RoutingState  string
	DeliveryState string
	Limit         int
	Offset        int
}

type SecurityEventPage struct {
	Events []SecurityEvent `json:"events"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type SecurityNotificationSettings struct {
	EnabledEventKinds     []string  `json:"enabled_event_kinds"`
	PrivacyLevel          string    `json:"privacy_level"`
	LookbackDays          int       `json:"lookback_days"`
	ThresholdFailures     int       `json:"threshold_failures"`
	ThresholdWindowSecond int       `json:"threshold_window_seconds"`
	InvalidTOTPThreshold  int       `json:"invalid_totp_threshold"`
	RetentionDays         int       `json:"retention_days"`
	Revision              int64     `json:"revision"`
	UpdatedAt             time.Time `json:"updated_at"`
	UpdatedBy             string    `json:"updated_by"`
}

type SecurityNotificationSettingsWrite struct {
	ExpectedRevision      int64    `json:"expected_revision"`
	EnabledEventKinds     []string `json:"enabled_event_kinds"`
	PrivacyLevel          string   `json:"privacy_level"`
	LookbackDays          int      `json:"lookback_days"`
	ThresholdFailures     int      `json:"threshold_failures"`
	ThresholdWindowSecond int      `json:"threshold_window_seconds"`
	InvalidTOTPThreshold  int      `json:"invalid_totp_threshold"`
	RetentionDays         int      `json:"retention_days"`
}
