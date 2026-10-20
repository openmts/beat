package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func (r *Router) sessionAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		state, err := r.security.State(request.Context())
		if err != nil {
			middleware.WriteAuthError(w, http.StatusInternalServerError, "authentication failed")
			return
		}
		if state.SetupRequired {
			middleware.BearerAuth(r.adminToken)(next).ServeHTTP(w, request)
			return
		}
		principal, ok := r.cookiePrincipal(w, request)
		if !ok {
			return
		}
		if middleware.StateChanging(request.Method) && !middleware.SameOrigin(request) {
			middleware.WriteAuthError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		ctx := middleware.WithAdminPrincipal(request.Context(), principal)
		audited := r.auditAdminRequest(next, request, &principal)
		audited.ServeHTTP(w, request.WithContext(ctx))
	})
}

func (r *Router) auditAdminRequest(
	next http.Handler, request *http.Request, principal *model.AdminPrincipal,
) http.Handler {
	if request.Pattern == "POST /api/v1/auth/logout" || request.Pattern == "POST /api/v1/auth/reauthenticate" ||
		request.Pattern == "GET /api/v1/auth/session" || request.Pattern == "GET /api/v1/auth/admin" {
		return next
	}
	return middleware.ObserveStatus(next, func(status int) {
		action, resourceID, atomic := administratorAuditPolicy(request, principal)
		if atomic && status < http.StatusBadRequest {
			return
		}
		input := administratorRequestAudit(request, principal, requestAuditDecision{action: action, resourceID: resourceID, atomic: atomic, status: status})
		err := r.security.RecordAudit(context.WithoutCancel(request.Context()), input)
		if err != nil {
			slog.ErrorContext(request.Context(), "administrator audit write failed",
				"request_id", middleware.RequestID(request.Context()), "error", err)
		}
	})
}

func auditAction(request *http.Request) string {
	if middleware.StateChanging(request.Method) {
		return "admin.mutation"
	}
	return "admin.read"
}

func requestIP(request *http.Request) string {
	return middleware.ClientIP(request)
}

func (r *Router) sessionWebSocketAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		state, err := r.security.State(request.Context())
		if err != nil {
			middleware.WriteAuthError(w, http.StatusInternalServerError, "authentication failed")
			return
		}
		if state.SetupRequired {
			middleware.WebSocketBearerAuth(r.adminToken)(next).ServeHTTP(w, request)
			return
		}
		principal, ok := r.cookiePrincipal(w, request)
		if !ok {
			return
		}
		if !middleware.SameOrigin(request) {
			middleware.WriteAuthError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		ctx := middleware.WithAdminPrincipal(request.Context(), principal)
		r.guardedAdminOperation(next).ServeHTTP(w, request.WithContext(ctx))
	})
}

func (r *Router) cookiePrincipal(
	w http.ResponseWriter, request *http.Request,
) (model.AdminPrincipal, bool) {
	cookie, err := request.Cookie(handler.AdminSessionCookieName)
	if err != nil || cookie.Value == "" {
		middleware.WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
		return model.AdminPrincipal{}, false
	}
	principal, err := r.security.Authenticate(request.Context(), cookie.Value)
	if err == nil {
		return principal, true
	}
	if !errors.Is(err, adminauth.ErrSessionInvalid) {
		// 存储不可用时失败关闭并如实回答 503：回 401 会把用户引向重新登录，而登录同样写不进数据库。
		slog.ErrorContext(request.Context(), "administrator session lookup failed", "error", err)
		middleware.WriteAuthError(w, http.StatusServiceUnavailable, "authentication unavailable")
		return model.AdminPrincipal{}, false
	}
	middleware.WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
	return model.AdminPrincipal{}, false
}

// ownerAdmin 要求 owner 角色（继承会话管理员认证）。
//
// 没有会话服务时无法证明 owner 身份，因此失败关闭：拒绝请求而不是放行或 panic。
func (r *Router) ownerAdmin(next http.Handler) http.Handler {
	if r.security == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			middleware.WriteAuthError(w, http.StatusForbidden, "administrator session is required")
		})
	}
	return r.sessionAdmin(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, ok := middleware.AdminPrincipal(request.Context())
		if !ok || principal.User.Role != model.AdminRoleOwner {
			middleware.WriteAuthError(w, http.StatusForbidden, "owner permission is required")
			return
		}
		next.ServeHTTP(w, request)
	}))
}

// recentAuthOwnerAdmin 要求 owner 角色且最近已本地重认证（敏感管理操作）。
func (r *Router) recentAuthOwnerAdmin(next http.Handler) http.Handler {
	return r.ownerAdmin(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, _ := middleware.AdminPrincipal(request.Context())
		if !principal.Session.RecentlyAuthenticated(r.security.Now()) {
			middleware.WriteAuthError(w, http.StatusPreconditionRequired, "recent authentication is required")
			return
		}
		next.ServeHTTP(w, request)
	}))
}
