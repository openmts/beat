package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
)

const runtimeSessionScanLimit = 100

// runtimeSessionCleanup 在会话被登出或撤销后取消该会话运行中的运行时诊断捕获。
func (router *Router) runtimeSessionCleanup(next http.Handler, sessions func(*http.Request) []string) http.Handler {
	if router.runtimeDiagnostics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(w, request)
		for _, id := range sessions(request) {
			if id != "" {
				router.runtimeDiagnostics.RevokeSession(id)
			}
		}
	})
}

func runtimeCurrentSession(request *http.Request) []string {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		return nil
	}
	return []string{principal.Session.ID}
}

func runtimePathSession(request *http.Request) []string {
	return []string{request.PathValue("id")}
}

// runtimeOtherSessions 枚举调用者本人除当前会话外的会话，用于批量撤销后的清理。
func (router *Router) runtimeOtherSessions(request *http.Request) []string {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok || router.security == nil {
		return nil
	}
	sessions, err := router.security.ListSessions(request.Context(), &principal, runtimeSessionScanLimit)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if !session.Current {
			ids = append(ids, session.ID)
		}
	}
	return ids
}
