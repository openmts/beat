package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

// registerAdminSecurityRoutes 注册登录安全事件历史、确认与通知策略接口。
func (router *Router) registerAdminSecurityRoutes(mux *http.ServeMux) {
	if router.security == nil {
		return
	}
	h := handler.NewAdminSecurityHandler(router.security)
	mux.Handle("GET /api/v1/admin/security/events", router.admin(http.HandlerFunc(h.HandleListEvents)))
	mux.Handle("GET /api/v1/admin/security/events/{id}", router.admin(http.HandlerFunc(h.HandleEventDetail)))
	mux.Handle("GET /api/v1/admin/security/events/{id}/source", router.recentAuthOwnerAdmin(http.HandlerFunc(h.HandleEventSource)))
	mux.Handle("GET /api/v1/admin/security/options", router.admin(http.HandlerFunc(h.HandleOptions)))
	mux.Handle("POST /api/v1/admin/security/events/{id}/ack", router.admin(http.HandlerFunc(h.HandleAcknowledgeEvent)))
	mux.Handle("GET /api/v1/admin/security/policy", router.ownerAdmin(http.HandlerFunc(h.HandlePolicy)))
	mux.Handle("PUT /api/v1/admin/security/policy", router.recentAuthOwnerAdmin(http.HandlerFunc(h.HandleUpdatePolicy)))
}
