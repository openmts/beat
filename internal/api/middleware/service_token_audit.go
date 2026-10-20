package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
)

const (
	serviceTokenAuditAction          = "service_token.request"
	serviceTokenAuditResource        = "service_scope"
	serviceTokenAuditUnauthenticated = "unauthenticated"
	serviceTokenAuditUnavailable     = "unavailable"
	serviceTokenAuditRateLimit       = "rate_limit"
	serviceTokenAuditHandlerError    = "handler_error"
	serviceTokenAuditFieldLimit      = 256
)

// serviceTokenPrivilegedReads 列出必须逐次留痕的只读权限。遥测类高频只读由
// last_used_at 与指标覆盖，避免审计表被轮询流量撑爆。
var serviceTokenPrivilegedReads = map[model.ServiceScope]bool{
	model.ServiceScopeAuditRead:           true,
	model.ServiceScopeBackupsReadMetadata: true,
	model.ServiceScopeBackupsDownload:     true,
	model.ServiceScopeHealthRead:          true,
}

// auditResponseWriter 记录处理结果状态码，供审计判定成功或失败。
type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *auditResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *auditResponseWriter) Write(payload []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(payload)
}

// Unwrap 保证 http.ResponseController 仍能访问原始 ResponseWriter。
func (writer *auditResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// serviceTokenAudit 绑定一次数据面请求的审计上下文。
type serviceTokenAudit struct {
	config    ServiceTokenAccessConfig
	request   *http.Request
	principal *model.ServicePrincipal
}

// privileged 只对变更请求与特权只读留痕；遥测类高频只读由指标与 last_used_at 覆盖。
func (audit serviceTokenAudit) privileged() bool {
	if audit.request.Method != http.MethodGet && audit.request.Method != http.MethodHead {
		return true
	}
	return serviceTokenPrivilegedReads[audit.config.Scope]
}

func (audit serviceTokenAudit) deny(reason string) {
	audit.record(model.AuditOutcomeFailure, reason)
}

func (audit serviceTokenAudit) record(outcome, reason string) {
	config, request := audit.config, audit.request
	if config.Audit == nil {
		return
	}
	event := model.ServiceAuditEvent{
		RequestID: RequestID(request.Context()), Action: serviceTokenAuditAction,
		ResourceType: serviceTokenAuditResourceType(config), ResourceID: serviceTokenAuditResourceID(config, request),
		Outcome: outcome, Reason: reason, Scope: string(config.Scope),
		Method: request.Method, Path: boundServiceAuditField(request.URL.Path),
		IPAddress: ClientIP(request), UserAgent: boundServiceAuditField(request.UserAgent()),
	}
	if audit.principal != nil {
		event.PrincipalID = audit.principal.ID
		event.PrincipalName = audit.principal.Name
	}
	// 审计写入不能因客户端断开而丢失；失败只记录日志，不放行未授权请求。
	ctx := context.WithoutCancel(request.Context())
	if err := config.Audit.RecordServiceAudit(ctx, event); err != nil {
		slog.Warn("service token audit write failed", "error", err,
			"outcome", outcome, "reason", reason, "path", event.Path)
	}
}

// admitFailedServiceToken 在认证失败写入审计前做来源维度准入，避免未知令牌的探测
// 流量把审计表撑大。被拒绝的请求只记指标，不写审计行。
func admitFailedServiceToken(config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request) bool {
	if config.FailureLimiter == nil {
		return true
	}
	decision := config.FailureLimiter.Allow("", ClientIP(request))
	if decision.Allowed {
		return true
	}
	observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
		metrics.ObserveServiceTokenRateLimit(serviceprincipal.RateLimitScopeSource)
	})
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(decision.RetryAfter)))
	WriteAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
	return false
}

func serviceTokenAuditResourceType(config ServiceTokenAccessConfig) string {
	if config.ResourceKind != "" {
		return string(config.ResourceKind)
	}
	return serviceTokenAuditResource
}

func serviceTokenAuditResourceID(config ServiceTokenAccessConfig, request *http.Request) string {
	if config.ResolveResource != nil {
		if id := config.ResolveResource(request); id != "" {
			return id
		}
	}
	return string(config.Scope)
}

func boundServiceAuditField(value string) string {
	if utf8.RuneCountInString(value) <= serviceTokenAuditFieldLimit {
		return value
	}
	return string([]rune(value)[:serviceTokenAuditFieldLimit])
}
