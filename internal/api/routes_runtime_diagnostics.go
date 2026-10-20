package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

// registerRuntimeDiagnosticsRoutes 只注册所有者会话可用、且不含服务令牌范围的诊断接口。
func (router *Router) registerRuntimeDiagnosticsRoutes(api *http.ServeMux) {
	if router.runtimeDiagnostics == nil {
		return
	}
	diagnostics := handler.NewRuntimeDiagnosticsHandler(router.runtimeDiagnostics, router.runtimeAudit)
	api.Handle("GET /api/v1/admin/runtime/summary",
		router.ownerAdmin(http.HandlerFunc(diagnostics.HandleSummary)))
	api.Handle("GET /api/v1/admin/runtime/captures",
		router.ownerAdmin(http.HandlerFunc(diagnostics.HandleList)))
	api.Handle("POST /api/v1/admin/runtime/captures",
		router.recentAuthOwnerAdmin(http.HandlerFunc(diagnostics.HandleCreate)))
	api.Handle("GET /api/v1/admin/runtime/captures/{id}/download",
		router.recentAuthOwnerAdmin(http.HandlerFunc(diagnostics.HandleDownload)))
	api.Handle("DELETE /api/v1/admin/runtime/captures/{id}",
		router.recentAuthOwnerAdmin(http.HandlerFunc(diagnostics.HandleDelete)))
}
