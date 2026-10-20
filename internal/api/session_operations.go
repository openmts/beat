package api

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
)

func (router *Router) guardedAdminOperation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, exists := middleware.AdminPrincipal(request.Context())
		if router.security == nil || !exists {
			next.ServeHTTP(w, request)
			return
		}
		guarded, release, err := router.security.GuardSession(request.Context(), principal)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, adminauth.ErrLiveSessionCapacity) {
				status = http.StatusTooManyRequests
			}
			middleware.WriteAuthError(w, status, "administrator connection is unavailable")
			return
		}
		defer release()
		next.ServeHTTP(w, request.WithContext(guarded))
	})
}
