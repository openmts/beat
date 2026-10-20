package handler

import (
	"context"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/store"
)

// themeMutationContext 确保 HTTP 写操作不会降级成服务内 system 调用。
func themeMutationContext(w http.ResponseWriter, request *http.Request) (context.Context, string, bool) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok || principal.User.ID == "" || principal.Session.ID == "" {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return nil, "", false
	}
	actor := store.ThemeActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}
	return store.WithThemeActor(request.Context(), actor), principal.User.ID, true
}
