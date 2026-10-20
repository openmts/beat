package adminauth

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (service *Service) RecordAudit(ctx context.Context, input AuditInput) error {
	event := service.auditEvent(ctx, input)
	return service.config.Store.CreateAuditEvent(ctx, &event)
}

func (service *Service) auditEvent(ctx context.Context, input AuditInput) model.AdminAuditEvent {
	metadata, _ := ctx.Value(auditMetadataKey{}).(AuditMetadata)
	if input.RequestID == "" {
		input.RequestID = metadata.RequestID
	}
	if input.IPAddress == "" {
		input.IPAddress = metadata.IPAddress
	}
	if input.UserAgent == "" {
		input.UserAgent = metadata.UserAgent
	}
	requestID := input.RequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: requestID, ActorType: model.AuditActorAdmin, Action: input.Action,
		ResourceType: input.ResourceType, ResourceID: input.ResourceID, Outcome: input.Outcome,
		DetailJSON: "{}", IPAddress: input.IPAddress, UserAgent: input.UserAgent,
		CreatedAt: service.now(),
	}
	if input.Principal != nil {
		event.ActorID = input.Principal.User.ID
		event.ActorUsername = input.Principal.User.Username
		event.SessionPrefix = input.Principal.Session.TokenPrefix
	}
	return event
}

func (service *Service) ListAuditEvents(
	ctx context.Context, principal *model.AdminPrincipal, filter model.AuditFilter,
) (model.AuditPage, error) {
	if principal == nil {
		return model.AuditPage{}, ErrForbidden
	}
	return service.config.Store.ListAuditEvents(ctx, filter)
}

// ListAuditEventsForService 供已通过 audit:read 校验的服务主体读取审计分页。
// 浏览器会话前缀属于交互凭证元数据，服务主体视图一律清空。
func (service *Service) ListAuditEventsForService(ctx context.Context, filter model.AuditFilter) (model.AuditPage, error) {
	page, err := service.config.Store.ListAuditEvents(ctx, filter)
	if err != nil {
		return model.AuditPage{}, err
	}
	for index := range page.Events {
		page.Events[index].SessionPrefix = ""
	}
	return page, nil
}

type auditMetadataKey struct{}

type AuditMetadata struct{ RequestID, IPAddress, UserAgent string }

func WithAuditMetadata(ctx context.Context, metadata AuditMetadata) context.Context {
	return context.WithValue(ctx, auditMetadataKey{}, metadata)
}

// serviceAuditDetail 只记录有界、非机密的上下文；禁止写入令牌明文、哈希或会话前缀。
type serviceAuditDetail struct {
	Reason string `json:"reason,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
}

const serviceAuditFieldLimit = 256

// RecordServiceAudit 记录服务令牌数据面的认证、授权与执行结果。
func (service *Service) RecordServiceAudit(ctx context.Context, input model.ServiceAuditEvent) error {
	detail, err := json.Marshal(serviceAuditDetail{
		Reason: input.Reason, Scope: input.Scope,
		Method: boundAuditField(input.Method), Path: boundAuditField(input.Path),
	})
	if err != nil {
		return fmt.Errorf("encode service audit detail: %w", err)
	}
	metadata, _ := ctx.Value(auditMetadataKey{}).(AuditMetadata)
	requestID := input.RequestID
	if requestID == "" {
		requestID = metadata.RequestID
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	ipAddress, userAgent := input.IPAddress, input.UserAgent
	if ipAddress == "" {
		ipAddress = metadata.IPAddress
	}
	if userAgent == "" {
		userAgent = metadata.UserAgent
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: requestID, ActorType: model.AuditActorService,
		ActorID: input.PrincipalID, ActorUsername: boundAuditField(input.PrincipalName),
		Action: input.Action, ResourceType: input.ResourceType,
		ResourceID: boundAuditField(input.ResourceID), Outcome: input.Outcome,
		DetailJSON: string(detail), IPAddress: ipAddress,
		UserAgent: boundAuditField(userAgent), CreatedAt: service.now(),
	}
	return service.config.Store.CreateAuditEvent(ctx, &event)
}

// boundAuditField 截断客户端可控字段，避免审计行被超长输入撑大。
func boundAuditField(value string) string {
	if utf8.RuneCountInString(value) <= serviceAuditFieldLimit {
		return value
	}
	runes := []rune(value)
	return string(runes[:serviceAuditFieldLimit])
}
