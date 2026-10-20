package adminauth

import (
	"context"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// requireActiveAdministrator 只接受已启用管理员的会话主体。
func requireActiveAdministrator(principal *model.AdminPrincipal) error {
	if principal == nil || !principal.User.Enabled {
		return ErrForbidden
	}
	return nil
}

// ListSecurityEvents 返回登录安全事件分页，任何已授权管理员都可读取。
func (service *Service) ListSecurityEvents(
	ctx context.Context, principal *model.AdminPrincipal, filter model.SecurityEventFilter,
) (model.SecurityEventPage, error) {
	if err := requireActiveAdministrator(principal); err != nil {
		return model.SecurityEventPage{}, err
	}
	return service.config.Store.ListSecurityEvents(ctx, filter)
}

// SecurityEvent 返回单条事件详情。
func (service *Service) SecurityEvent(
	ctx context.Context, principal *model.AdminPrincipal, id string,
) (model.SecurityEvent, error) {
	if err := requireActiveAdministrator(principal); err != nil {
		return model.SecurityEvent{}, err
	}
	if id == "" {
		return model.SecurityEvent{}, store.ErrSecurityEventNotFound
	}
	return service.config.Store.GetSecurityEvent(ctx, id)
}

// AcknowledgeSecurityEvent 记录确认元数据，不修改不可变的事件本体。
func (service *Service) AcknowledgeSecurityEvent(
	ctx context.Context, principal *model.AdminPrincipal, id, note string,
) (model.SecurityEvent, error) {
	if err := requireActiveAdministrator(principal); err != nil {
		return model.SecurityEvent{}, err
	}
	audit := service.auditEvent(ctx, AuditInput{Principal: principal, Action: "security.event.acknowledge",
		ResourceType: "security_event", ResourceID: id, Outcome: model.AuditOutcomeSuccess})
	return service.config.Store.AcknowledgeSecurityEvent(ctx, store.SecurityEventAck{EventID: id,
		ActorID: principal.User.ID, Note: note, At: service.now(), Audit: audit})
}

// SecurityPolicy 读取登录安全通知策略；只有 owner 能查看阈值与隐私配置。
func (service *Service) SecurityPolicy(
	ctx context.Context, principal *model.AdminPrincipal,
) (model.SecurityNotificationSettings, error) {
	if err := service.RequireOwner(principal); err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	return service.config.Store.SecurityNotificationSettings(ctx)
}

// UpdateSecurityPolicy 更新策略，要求 owner 角色与最近一次身份确认。
func (service *Service) UpdateSecurityPolicy(
	ctx context.Context, principal *model.AdminPrincipal, write model.SecurityNotificationSettingsWrite,
) (model.SecurityNotificationSettings, error) {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	audit := service.auditEvent(ctx, AuditInput{Principal: principal, Action: "security.policy.update",
		ResourceType: "security_settings", ResourceID: "security_notification_settings", Outcome: model.AuditOutcomeSuccess})
	return service.config.Store.UpdateSecurityNotificationSettings(ctx, store.SecuritySettingsChange{
		Write: write, Audit: audit, UpdatedBy: principal.User.ID, Now: service.now()})
}

// SecurityMetrics 汇总登录安全持久化规模，供可观测性端点使用。
func (service *Service) SecurityMetrics(ctx context.Context) (store.SecurityDiagnostics, error) {
	thresholds, err := service.config.Store.CountSecurityThresholds(ctx)
	if err != nil {
		return store.SecurityDiagnostics{}, err
	}
	fingerprints, err := service.config.Store.CountSecurityFingerprints(ctx)
	if err != nil {
		return store.SecurityDiagnostics{}, err
	}
	return store.SecurityDiagnostics{Thresholds: thresholds, Fingerprints: fingerprints}, nil
}

// SecurityEventSource 揭示事件的规范来源地址：仅 owner、已重认证且策略选择完整来源时可用。
// 事件表只保存截断前缀，完整地址取自同一次登录写入的审计记录。每次揭示都写入审计。
func (service *Service) SecurityEventSource(
	ctx context.Context, principal *model.AdminPrincipal, id string,
) (model.SecurityEventSource, error) {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return model.SecurityEventSource{}, err
	}
	if id == "" {
		return model.SecurityEventSource{}, store.ErrSecurityEventNotFound
	}
	settings, err := service.config.Store.SecurityNotificationSettings(ctx)
	if err != nil {
		return model.SecurityEventSource{}, err
	}
	if settings.PrivacyLevel != model.SecurityPrivacyFullSource {
		return model.SecurityEventSource{}, ErrForbidden
	}
	audit := service.auditEvent(ctx, AuditInput{Principal: principal, Action: "security.event.source_reveal",
		ResourceType: "security_event", ResourceID: id, Outcome: model.AuditOutcomeSuccess})
	return service.config.Store.RevealSecurityEventSource(ctx, store.SecurityEventSourceReveal{
		EventID: id, ActorID: principal.User.ID, At: service.now(), Audit: audit})
}
