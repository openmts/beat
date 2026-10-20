package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/serviceprincipalhttp"
)

// WithServicePrincipals 注册服务令牌管理路由。
func WithServicePrincipals(handler *serviceprincipalhttp.Handler) RouterOption {
	return func(router *Router) { router.servicePrincipals = handler }
}

func (r *Router) registerServicePrincipalRoutes(mux *http.ServeMux) {
	if r.servicePrincipals == nil {
		return
	}
	r.servicePrincipals.RegisterProtected(mux, serviceprincipalhttp.Protection{
		OwnerRead:  r.serviceTokenGuard(false),
		OwnerWrite: r.serviceTokenGuard(true),
	})
}

// serviceTokenGuard 要求 owner 身份；写入路径还要求近期重认证。
func (r *Router) serviceTokenGuard(recent bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if r.security == nil {
				middleware.WriteAuthError(w, http.StatusServiceUnavailable, "administrator session is unavailable")
				return
			}
			guarded := r.ownerAdmin(next)
			if recent {
				guarded = r.recentAuthOwnerAdmin(next)
			}
			guarded.ServeHTTP(w, request)
		})
	}
}
