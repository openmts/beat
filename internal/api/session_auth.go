package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func (r *Router) sessionAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
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
		outcome := model.AuditOutcomeSuccess
		if status >= http.StatusBadRequest {
			outcome = model.AuditOutcomeFailure
		}
		err := r.security.RecordAudit(context.WithoutCancel(request.Context()), adminauth.AuditInput{
			RequestID: middleware.RequestID(request.Context()), Principal: principal,
			Action: auditAction(request), ResourceType: "http_route",
			ResourceID: request.Pattern, Outcome: outcome, IPAddress: requestIP(request),
			UserAgent: request.UserAgent(),
		})
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
		next.ServeHTTP(w, request.WithContext(ctx))
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
	if err != nil {
		middleware.WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
		return model.AdminPrincipal{}, false
	}
	return principal, true
}
