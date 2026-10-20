package model

import "time"

// ServiceScope 是服务令牌可授予的固定权限常量；令牌不能请求通配符权限。
type ServiceScope string

const (
	ServiceScopeNodesRead              ServiceScope = "nodes:read"
	ServiceScopeNodesWritePresentation ServiceScope = "nodes:write-presentation"
	ServiceScopeMetricsRead            ServiceScope = "metrics:read"
	ServiceScopeNetworkTasksRead       ServiceScope = "network-tasks:read"
	ServiceScopeNetworkTasksRunTest    ServiceScope = "network-tasks:run-test"
	ServiceScopeAlertsRead             ServiceScope = "alerts:read"
	ServiceScopeNotificationTestSend   ServiceScope = "notification-test:send"
	ServiceScopeTrafficReportsRead     ServiceScope = "traffic-reports:read"
	ServiceScopeTrafficReportsRunTest  ServiceScope = "traffic-reports:run-test"
	ServiceScopeBackupsCreate          ServiceScope = "backups:create"
	ServiceScopeBackupsReadMetadata    ServiceScope = "backups:read-metadata"
	ServiceScopeBackupsDownload        ServiceScope = "backups:download"
	ServiceScopeAuditRead              ServiceScope = "audit:read"
	ServiceScopeHealthRead             ServiceScope = "health:read"
)

// ServiceScopeCatalog 是创建或轮换服务令牌时允许选择的完整权限集合。
var ServiceScopeCatalog = []ServiceScope{
	ServiceScopeNodesRead,
	ServiceScopeNodesWritePresentation,
	ServiceScopeMetricsRead,
	ServiceScopeNetworkTasksRead,
	ServiceScopeNetworkTasksRunTest,
	ServiceScopeAlertsRead,
	ServiceScopeNotificationTestSend,
	ServiceScopeTrafficReportsRead,
	ServiceScopeTrafficReportsRunTest,
	ServiceScopeBackupsCreate,
	ServiceScopeBackupsReadMetadata,
	ServiceScopeBackupsDownload,
	ServiceScopeAuditRead,
	ServiceScopeHealthRead,
}

// Valid 判断权限是否属于服务端定义的固定集合。
func (scope ServiceScope) Valid() bool {
	for _, candidate := range ServiceScopeCatalog {
		if scope == candidate {
			return true
		}
	}
	return false
}

// ServiceResourceKind 是服务令牌可约束的资源类型。
type ServiceResourceKind string

const (
	ServiceResourceNode    ServiceResourceKind = "node"
	ServiceResourceGroup   ServiceResourceKind = "group"
	ServiceResourceChannel ServiceResourceKind = "channel"
)

// Valid 判断资源类型是否受支持。
func (kind ServiceResourceKind) Valid() bool {
	switch kind {
	case ServiceResourceNode, ServiceResourceGroup, ServiceResourceChannel:
		return true
	default:
		return false
	}
}

// ServiceResource 把令牌约束到具体节点、分组或通知渠道。
type ServiceResource struct {
	Kind ServiceResourceKind `json:"kind"`
	ID   string              `json:"id"`
}

// ServiceVisibility 是服务令牌可读取的数据可见范围。
type ServiceVisibility string

const (
	ServiceVisibilityPublic  ServiceVisibility = "public"
	ServiceVisibilityPrivate ServiceVisibility = "private"
)

// Valid 判断可见范围是否受支持。
func (visibility ServiceVisibility) Valid() bool {
	return visibility == ServiceVisibilityPublic || visibility == ServiceVisibilityPrivate
}

const (
	// ServiceTokenPrefix 是服务令牌的可识别版本前缀。
	ServiceTokenPrefix = "beat_service_v1_"
	// ServiceTokenDisplayLength 是展示前缀保留的随机字符数。
	ServiceTokenDisplayLength = 8

	MaximumServiceTokenName      = 128
	MaximumServiceTokenPurpose   = 512
	MaximumServiceTokenResources = 64
	MaximumServiceTokenCIDRs     = 16
	MaximumServiceTokenIDLength  = 128
)

const (
	// DefaultServiceTokenLifetime 是未做近期重认证时的绝对有效期上限。
	DefaultServiceTokenLifetime = 90 * 24 * time.Hour
	// ExtendedServiceTokenLifetime 仅允许拥有近期重认证的 owner 授予。
	ExtendedServiceTokenLifetime = 365 * 24 * time.Hour
	// MaximumServiceTokenRotationOverlap 限制轮换重叠窗口。
	MaximumServiceTokenRotationOverlap = 24 * time.Hour
)

// ServicePrincipal 是服务令牌的持久化主体；明文令牌从不落库。
type ServicePrincipal struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Purpose                string            `json:"purpose"`
	TokenPrefix            string            `json:"prefix"`
	TokenHash              []byte            `json:"-"`
	Visibility             ServiceVisibility `json:"visibility"`
	SourceCIDRs            []string          `json:"source_cidrs"`
	Scopes                 []ServiceScope    `json:"scopes"`
	Resources              []ServiceResource `json:"resources,omitempty"`
	ExpiresAt              time.Time         `json:"expires_at"`
	RevokedAt              *time.Time        `json:"revoked_at,omitempty"`
	RevokedReason          string            `json:"revoked_reason,omitempty"`
	SupersededAt           *time.Time        `json:"superseded_at,omitempty"`
	RotationOverlapSeconds int               `json:"rotation_overlap_seconds"`
	RotatedFrom            string            `json:"rotated_from,omitempty"`
	Revision               int               `json:"revision"`
	CreatedBy              string            `json:"created_by"`
	CreatedAt              time.Time         `json:"created_at"`
	LastUsedAt             *time.Time        `json:"last_used_at,omitempty"`
}
