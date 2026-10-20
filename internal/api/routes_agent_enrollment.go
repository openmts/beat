package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func (r *Router) registerAgentEnrollmentRoutes(api *http.ServeMux) {
	enrollment := handler.NewAgentEnrollmentHandler(r.nodeStore)
	api.Handle("POST /api/v1/admin/agent-enrollment-tokens", enrollmentPrivate(r.admin(http.HandlerFunc(enrollment.HandleIssue))))
	api.Handle("POST /api/v1/agents/join", enrollmentPrivate(http.HandlerFunc(enrollment.HandleJoin)))
	if r.enrollment == nil {
		return
	}
	r.registerAgentDiscoveryRoutes(api)
}

// registerAgentDiscoveryRoutes 注册自动发现式入网的管理端路由。
func (r *Router) registerAgentDiscoveryRoutes(api *http.ServeMux) {
	r.registerAgentEnrollmentAdminRoutes(api)
	r.registerAgentEnrollmentProtocolRoutes(api)
}

func (r *Router) registerAgentEnrollmentAdminRoutes(api *http.ServeMux) {
	admin := handler.NewAgentEnrollmentAdminHandler(r.enrollment)
	admin.SetEnrollmentMetrics(r.enrollmentMetrics)
	api.Handle("GET /api/v1/admin/agent-enrollment/grants",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleListGrants)))
	api.Handle("POST /api/v1/admin/agent-enrollment/grants",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleCreateGrant)))
	api.Handle("GET /api/v1/admin/agent-enrollment/grants/{id}",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleGetGrant)))
	api.Handle("PUT /api/v1/admin/agent-enrollment/grants/{id}",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleUpdateGrant)))
	api.Handle("POST /api/v1/admin/agent-enrollment/grants/{id}/rotate",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleRotateGrantSecret)))
	api.Handle("POST /api/v1/admin/agent-enrollment/grants/{id}/revoke",
		r.recentAuthOwnerAdmin(http.HandlerFunc(admin.HandleRevokeGrant)))
	api.Handle("GET /api/v1/admin/agent-enrollment/requests",
		r.ownerAdmin(http.HandlerFunc(admin.HandleListRequests)))
	api.Handle("GET /api/v1/admin/agent-enrollment/requests/{id}",
		r.ownerAdmin(http.HandlerFunc(admin.HandleGetRequest)))
	api.Handle("POST /api/v1/admin/agent-enrollment/requests/{id}/approve",
		r.ownerAdmin(http.HandlerFunc(admin.HandleApproveRequest)))
	api.Handle("POST /api/v1/admin/agent-enrollment/requests/{id}/reject",
		r.ownerAdmin(http.HandlerFunc(admin.HandleRejectRequest)))
}

func enrollmentPrivate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request)
	})
}
