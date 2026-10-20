package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
)

// ServiceTokenAccess 描述数据端点的服务令牌访问配置。
type ServiceTokenAccess struct {
	Authenticator middleware.ServiceTokenAuthenticator
	Limiter       *serviceprincipal.Limiter
	// FailureLimiter 只限制无法认证的请求来源，避免失败认证的审计写放大。
	FailureLimiter *serviceprincipal.Limiter
	Metrics        middleware.ServiceTokenMetrics
	// Visibility 解析受控资源是否公开；nil 时 public 可见性令牌只能访问公开端点。
	Visibility middleware.ServiceResourceVisibility
	// Audit 记录数据面拒绝与特权操作；nil 表示不落审计。
	Audit middleware.ServiceTokenAudit
}

// WithServiceTokenAccess 注入服务令牌认证器、限流器、可见性与指标；nil 认证器表示不启用。
func WithServiceTokenAccess(access ServiceTokenAccess) RouterOption {
	return func(router *Router) {
		if access.Authenticator != nil {
			router.serviceTokenAccess = &access
		}
	}
}

// serviceTokenRouteSpec 描述单个数据端点的服务令牌策略。
type serviceTokenRouteSpec struct {
	Scope   model.ServiceScope
	Kind    model.ServiceResourceKind
	Resolve middleware.ServiceResourceResolver
	Browser func(http.Handler) http.Handler
	// Private 表示端点数据本身非公开，public 可见性令牌一律拒绝。
	Private bool
}

func (r *Router) serviceTokenEndpointGuard(spec serviceTokenRouteSpec) func(http.Handler) http.Handler {
	access := r.serviceTokenAccess
	return func(next http.Handler) http.Handler {
		if access == nil {
			if spec.Browser == nil {
				return next
			}
			return spec.Browser(next)
		}
		return middleware.ServiceTokenAccess(middleware.ServiceTokenAccessConfig{
			Authenticator: access.Authenticator, Limiter: access.Limiter, Metrics: access.Metrics,
			Audit: access.Audit, FailureLimiter: access.FailureLimiter,
			Scope: spec.Scope, ResourceKind: spec.Kind, ResolveResource: spec.Resolve,
			ResourceVisibility: access.Visibility, RequirePrivate: spec.Private, Browser: spec.Browser,
		})(next)
	}
}

// serviceTokenPrivateRoute 用于非公开数据端点；浏览器回退沿用默认管理员守卫。
func (r *Router) serviceTokenPrivateRoute(scope model.ServiceScope, browser func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	if browser == nil {
		browser = r.admin
	}
	return r.serviceTokenEndpointGuard(serviceTokenRouteSpec{Scope: scope, Browser: browser, Private: true})
}

// serviceTokenPrivateResourceRoute 额外按请求中的资源 ID 校验，并拒绝 public 可见性令牌。
func (r *Router) serviceTokenPrivateResourceRoute(
	scope model.ServiceScope, kind model.ServiceResourceKind, resolve middleware.ServiceResourceResolver,
	browser func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	if browser == nil {
		browser = r.admin
	}
	return r.serviceTokenEndpointGuard(serviceTokenRouteSpec{Scope: scope, Kind: kind, Resolve: resolve, Browser: browser, Private: true})
}

// serviceTokenResourceRoute 按资源公开性放行 public 可见性令牌，用于公开节点数据端点。
func (r *Router) serviceTokenResourceRoute(
	scope model.ServiceScope, kind model.ServiceResourceKind, resolve middleware.ServiceResourceResolver,
	browser func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	return r.serviceTokenEndpointGuard(serviceTokenRouteSpec{Scope: scope, Kind: kind, Resolve: resolve, Browser: browser})
}
