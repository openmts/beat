package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/model"
)

// 自动发现式入网的审计动作名：管理端动作保留已认证操作者，公开协议路径使用匿名主体。
const (
	EnrollmentActionGrantCreate     = "agent.enrollment.grant.create"
	EnrollmentActionGrantUpdate     = "agent.enrollment.grant.update"
	EnrollmentActionGrantRotate     = "agent.enrollment.grant.rotate"
	EnrollmentActionGrantRevoke     = "agent.enrollment.grant.revoke"
	EnrollmentActionRequestCreate   = "agent.enrollment.request.create"
	EnrollmentActionRequestResume   = "agent.enrollment.request.resume"
	EnrollmentActionRequestApprove  = "agent.enrollment.request.approve"
	EnrollmentActionRequestReject   = "agent.enrollment.request.reject"
	EnrollmentActionRequestConflict = "agent.enrollment.request.conflict"
	EnrollmentActionRequestExpire   = "agent.enrollment.request.expire"
	EnrollmentActionClaim           = "agent.enrollment.claim"
	EnrollmentActionClaimRecover    = "agent.enrollment.claim.recover"
)

// 入网审计的资源类型与匿名主体名：匿名主体只有角色名，不含任何机器标识。
const (
	enrollmentGrantResourceType   = "agent_enrollment_grant"
	enrollmentRequestResourceType = "agent_enrollment_request"
	enrollmentAnonymousActor      = "agent"
)

// 审计列的长度上限与内置展示常量保持一致，超长输入先截断再落库。
const (
	enrollmentAuditRequestIDLimit = 128
	enrollmentAuditIPLimit        = 128
	enrollmentAuditUserAgentLimit = 512
)

// EnrollmentAuditContext 是一次入网变更的来源信息；管理端从会话取，协议路径留空。
type EnrollmentAuditContext struct {
	ActorID       string
	ActorUsername string
	RequestID     string
	IPAddress     string
	UserAgent     string
}

// enrollmentAuditContextKey 让调用方通过请求上下文携带审计来源，避免改写共享签名。
type enrollmentAuditContextKey struct{}

// WithEnrollmentAuditContext 把审计来源放进上下文；协议路径只填来源地址与请求 ID。
func WithEnrollmentAuditContext(ctx context.Context, audit EnrollmentAuditContext) context.Context {
	return context.WithValue(ctx, enrollmentAuditContextKey{}, audit)
}

// enrollmentAuditFrom 读取上下文的审计来源；未设置时按匿名协议主体记录。
func enrollmentAuditFrom(ctx context.Context) EnrollmentAuditContext {
	audit, _ := ctx.Value(enrollmentAuditContextKey{}).(EnrollmentAuditContext)
	return audit
}

// enrollmentAuditEvent 是一条待写入的入网审计事件；Detail 必须是已经脱敏的字段。
type enrollmentAuditEvent struct {
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Audit        EnrollmentAuditContext
	Detail       map[string]any
	Now          time.Time
}

// writeEnrollmentAudit 与调用方的应用变更共享事务，避免成功响应但审计落盘失败。
func writeEnrollmentAudit(ctx context.Context, db schemaExecutor, event enrollmentAuditEvent) error {
	detail, err := encodeEnrollmentAuditDetail(event.Detail)
	if err != nil {
		return err
	}
	outcome := event.Outcome
	if outcome == "" {
		outcome = model.AuditOutcomeSuccess
	}
	// 已识别主体缺少展示名时回落到主体 ID，避免把管理端动作记成匿名协议主体。
	username := strings.TrimSpace(event.Audit.ActorUsername)
	if username == "" {
		username = strings.TrimSpace(event.Audit.ActorID)
	}
	if username == "" {
		username = enrollmentAnonymousActor
	}
	eventID, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("create enrollment audit identifier: %w", err)
	}
	return insertAuditEvent(ctx, db, &model.AdminAuditEvent{
		ID: eventID.String(), RequestID: enrollmentAuditField(event.Audit.RequestID, enrollmentAuditRequestIDLimit),
		ActorID:       enrollmentAuditField(event.Audit.ActorID, enrollmentAuditRequestIDLimit),
		ActorUsername: enrollmentAuditField(username, enrollmentAuditRequestIDLimit),
		Action:        event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID,
		Outcome: outcome, DetailJSON: detail,
		IPAddress: enrollmentAuditField(event.Audit.IPAddress, enrollmentAuditIPLimit),
		UserAgent: enrollmentAuditField(event.Audit.UserAgent, enrollmentAuditUserAgentLimit),
		CreatedAt: event.Now.UTC(),
	})
}

// encodeEnrollmentAuditDetail 编码有界明细；空明细写成空对象而不是 NULL。
func encodeEnrollmentAuditDetail(detail map[string]any) (string, error) {
	if len(detail) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("encode enrollment audit detail: %w", err)
	}
	return string(encoded), nil
}

// enrollmentAuditField 去掉非法 UTF-8 并按上限截断，保证审计列始终可写。
func enrollmentAuditField(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

// enrollmentGrantAuditInput 汇总一条授权审计事件的字段，避免长位置参数列表。
type enrollmentGrantAuditInput struct {
	Action  string
	GrantID string
	Detail  map[string]any
	Audit   EnrollmentAuditContext
	Now     time.Time
}

// enrollmentGrantAuditEvent 把输入归一化为可写入的审计事件。
func enrollmentGrantAuditEvent(input enrollmentGrantAuditInput) enrollmentAuditEvent {
	return enrollmentAuditEvent{Action: input.Action, ResourceType: enrollmentGrantResourceType,
		ResourceID: input.GrantID, Audit: input.Audit, Detail: input.Detail, Now: input.Now}
}

// enrollmentRequestAuditInput 汇总一条请求审计事件的字段。
type enrollmentRequestAuditInput struct {
	Action    string
	RequestID string
	Detail    map[string]any
	Audit     EnrollmentAuditContext
	Now       time.Time
}

// enrollmentRequestAuditEvent 把输入归一化为可写入的审计事件。
func enrollmentRequestAuditEvent(input enrollmentRequestAuditInput) enrollmentAuditEvent {
	return enrollmentAuditEvent{Action: input.Action, ResourceType: enrollmentRequestResourceType,
		ResourceID: input.RequestID, Audit: input.Audit, Detail: input.Detail, Now: input.Now}
}

// enrollmentGrantPolicyDetail 是授权策略的脱敏明细：不含秘密、哈希或前缀。
func enrollmentGrantPolicyDetail(policy agentenrollment.GrantPolicy) map[string]any {
	return map[string]any{
		"name": policy.Name, "mode": string(policy.Mode), "enabled": policy.Enabled,
		"maximum_enrollments": policy.MaximumEnrollments,
		"expires_at":          policy.ExpiresAt.UTC().Format(time.RFC3339),
	}
}
