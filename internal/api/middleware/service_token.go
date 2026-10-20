package middleware

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
)

type servicePrincipalKey struct{}

// WithServicePrincipal 把已认证的服务主体写入请求上下文。
func WithServicePrincipal(ctx context.Context, principal model.ServicePrincipal) context.Context {
	return context.WithValue(ctx, servicePrincipalKey{}, principal)
}

// ServicePrincipalFrom 读取已认证的服务主体。
func ServicePrincipalFrom(ctx context.Context) (model.ServicePrincipal, bool) {
	principal, ok := ctx.Value(servicePrincipalKey{}).(model.ServicePrincipal)
	return principal, ok
}

// ServiceTokenAuthenticator 校验 Bearer 服务令牌并返回持久化主体。
type ServiceTokenAuthenticator interface {
	Authenticate(context.Context, string, netip.Addr) (*model.ServicePrincipal, error)
}

// ServiceTokenMetrics 记录认证、授权与限流结果；实现方只接受有界枚举标签。
type ServiceTokenMetrics interface {
	ObserveServiceTokenAuth(outcome string)
	ObserveServiceTokenDenial(reason string)
	ObserveServiceTokenRateLimit(scope string)
	ObserveServiceTokenLatency(scope string, duration time.Duration)
}

// ServiceTokenAudit 记录服务令牌数据面的认证、授权与执行结果。
// 实现方不得把令牌明文、令牌哈希或会话前缀写入审计。
type ServiceTokenAudit interface {
	RecordServiceAudit(context.Context, model.ServiceAuditEvent) error
}

// ServiceResourceResolver 从请求解析受控资源 ID；返回空串表示只校验权限集合。
type ServiceResourceResolver func(*http.Request) string

// ServiceResourceVisibility 解析受控资源是否对外公开，供 public 可见性令牌判定。
type ServiceResourceVisibility func(context.Context, model.ServiceResourceKind, string) (bool, error)

// ServicePathResource 使用 ServeMux 路径变量解析资源 ID。
func ServicePathResource(name string) ServiceResourceResolver {
	return func(request *http.Request) string { return request.PathValue(name) }
}

// ServiceTokenAccessConfig 描述一个数据端点的服务令牌访问策略。
type ServiceTokenAccessConfig struct {
	Authenticator ServiceTokenAuthenticator
	Limiter       *serviceprincipal.Limiter
	Metrics       ServiceTokenMetrics
	// Audit 记录拒绝与特权操作；nil 表示该端点不落审计。
	Audit ServiceTokenAudit
	// FailureLimiter 只对无法认证的请求做来源准入；nil 表示不做额外准入。
	FailureLimiter  *serviceprincipal.Limiter
	Scope           model.ServiceScope
	ResourceKind    model.ServiceResourceKind
	ResolveResource ServiceResourceResolver
	Browser         func(http.Handler) http.Handler
	// ResourceVisibility 解析资源公开性；nil 表示端点没有可解析的公开资源。
	ResourceVisibility ServiceResourceVisibility
	// RequirePrivate 表示端点数据本身非公开，public 可见性令牌一律拒绝。
	RequirePrivate bool
}

const (
	serviceTokenOutcomeAuthenticated = "authenticated"
	serviceTokenOutcomeDenied        = "denied"
	serviceTokenOutcomeUnavailable   = "unavailable"
	serviceTokenDenialScope          = "scope"
	serviceTokenDenialResource       = "resource"
	serviceTokenDenialVisibility     = "visibility"
)

// ServiceTokenAccess 在 Bearer 服务令牌存在时按 scope 与资源约束放行，否则回退到浏览器守卫。
func ServiceTokenAccess(config ServiceTokenAccessConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fallback := next
		if config.Browser != nil {
			fallback = config.Browser(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			token, ok := bearerToken(request.Header.Get("Authorization"))
			if !ok || !strings.HasPrefix(token, model.ServiceTokenPrefix) || config.Authenticator == nil {
				fallback.ServeHTTP(w, request)
				return
			}
			started := time.Now()
			defer func() {
				observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
					metrics.ObserveServiceTokenLatency(string(config.Scope), time.Since(started))
				})
			}()
			principal, ok := authenticateServiceToken(config, w, request, token)
			if !ok {
				return
			}
			if !permitServiceToken(config, w, request, principal) {
				return
			}
			if !limitServiceToken(config, w, request, principal) {
				return
			}
			audit := serviceTokenAudit{config: config, request: request, principal: principal}
			if !audit.privileged() {
				next.ServeHTTP(w, request.WithContext(WithServicePrincipal(request.Context(), *principal)))
				return
			}
			writer := &auditResponseWriter{ResponseWriter: w}
			next.ServeHTTP(writer, request.WithContext(WithServicePrincipal(request.Context(), *principal)))
			if writer.status >= http.StatusBadRequest {
				audit.deny(serviceTokenAuditHandlerError)
				return
			}
			audit.record(model.AuditOutcomeSuccess, "")
		})
	}
}

func authenticateServiceToken(config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request, token string) (*model.ServicePrincipal, bool) {
	principal, err := config.Authenticator.Authenticate(request.Context(), token, clientAddress(request))
	if err == nil {
		observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
			metrics.ObserveServiceTokenAuth(serviceTokenOutcomeAuthenticated)
		})
		return principal, true
	}
	if errors.Is(err, serviceprincipal.ErrUnavailable) {
		observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
			metrics.ObserveServiceTokenAuth(serviceTokenOutcomeUnavailable)
		})
		if !admitFailedServiceToken(config, w, request) {
			return nil, false
		}
		serviceTokenAudit{config: config, request: request}.deny(serviceTokenAuditUnavailable)
		WriteAuthError(w, http.StatusServiceUnavailable, "authentication is unavailable")
		return nil, false
	}
	observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
		metrics.ObserveServiceTokenAuth(serviceTokenOutcomeDenied)
	})
	if !admitFailedServiceToken(config, w, request) {
		return nil, false
	}
	serviceTokenAudit{config: config, request: request}.deny(serviceTokenAuditUnauthenticated)
	WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
	return nil, false
}

func permitServiceToken(config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request, principal *model.ServicePrincipal) bool {
	resourceID := ""
	if config.ResolveResource != nil {
		resourceID = config.ResolveResource(request)
	}
	if !slices.Contains(principal.Scopes, config.Scope) {
		observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
			metrics.ObserveServiceTokenDenial(serviceTokenDenialScope)
		})
		serviceTokenAudit{config: config, request: request, principal: principal}.deny(serviceTokenDenialScope)
		WriteAuthError(w, http.StatusForbidden, "service token is not permitted")
		return false
	}
	required := serviceprincipal.Request{Scope: config.Scope, Kind: config.ResourceKind, ID: resourceID}
	if !serviceprincipal.Permits(*principal, required) {
		observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
			metrics.ObserveServiceTokenDenial(serviceTokenDenialResource)
		})
		serviceTokenAudit{config: config, request: request, principal: principal}.deny(serviceTokenDenialResource)
		WriteAuthError(w, http.StatusForbidden, "service token is not permitted")
		return false
	}
	return permitServiceTokenVisibility(config, w, request, principal, resourceID)
}

// permitServiceTokenVisibility 在权限与资源约束之后交叉 public/private 可见性。
// 解析失败按依赖不可用处理（503），不把私有数据降级放行。
func permitServiceTokenVisibility(
	config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request,
	principal *model.ServicePrincipal, resourceID string,
) bool {
	if principal.Visibility == model.ServiceVisibilityPrivate {
		return true
	}
	if config.RequirePrivate {
		denyServiceTokenVisibility(config, w, request, principal)
		return false
	}
	if config.ResourceVisibility == nil {
		// 端点未声明可见性概念时按公开数据对待；非公开端点必须显式标记 RequirePrivate
		// 或提供可见性解析器。
		return true
	}
	if resourceID == "" {
		denyServiceTokenVisibility(config, w, request, principal)
		return false
	}
	public, err := config.ResourceVisibility(request.Context(), config.ResourceKind, resourceID)
	if err != nil {
		observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
			metrics.ObserveServiceTokenAuth(serviceTokenOutcomeUnavailable)
		})
		WriteAuthError(w, http.StatusServiceUnavailable, "authorization is unavailable")
		return false
	}
	if !public {
		denyServiceTokenVisibility(config, w, request, principal)
		return false
	}
	return true
}

func denyServiceTokenVisibility(config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request, principal *model.ServicePrincipal) {
	observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
		metrics.ObserveServiceTokenDenial(serviceTokenDenialVisibility)
	})
	serviceTokenAudit{config: config, request: request, principal: principal}.deny(serviceTokenDenialVisibility)
	WriteAuthError(w, http.StatusForbidden, "service token is not permitted")
}

func limitServiceToken(config ServiceTokenAccessConfig, w http.ResponseWriter, request *http.Request, principal *model.ServicePrincipal) bool {
	if config.Limiter == nil {
		return true
	}
	decision := config.Limiter.Allow(principal.ID, ClientIP(request))
	if decision.Allowed {
		return true
	}
	scope := decision.Scope
	observeServiceToken(config.Metrics, func(metrics ServiceTokenMetrics) {
		metrics.ObserveServiceTokenRateLimit(scope)
	})
	serviceTokenAudit{config: config, request: request, principal: principal}.deny(serviceTokenAuditRateLimit)
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(decision.RetryAfter)))
	WriteAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
	return false
}

func retryAfterSeconds(wait time.Duration) int {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func clientAddress(request *http.Request) netip.Addr {
	address, err := netip.ParseAddr(strings.TrimSpace(ClientIP(request)))
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}

func observeServiceToken(metrics ServiceTokenMetrics, record func(ServiceTokenMetrics)) {
	if metrics == nil {
		return
	}
	record(metrics)
}
