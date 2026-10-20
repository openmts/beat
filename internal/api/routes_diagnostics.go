package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

type DiagnosticRouteConfig struct {
	Diagnostics  handler.Diagnostics
	ProtectAdmin func(http.Handler) http.Handler
}

// RegisterDiagnosticRoutes 只注册管理员控制面与每节点 Agent 端点，没有公共路由。
func RegisterDiagnosticRoutes(mux *http.ServeMux, config DiagnosticRouteConfig) {
	if config.Diagnostics == nil || config.ProtectAdmin == nil {
		return
	}
	h := handler.NewDiagnosticHandler(config.Diagnostics)
	for path, handle := range diagnosticAdminRoutes(h) {
		mux.Handle(path, config.ProtectAdmin(handle))
	}
	for path, handle := range diagnosticAgentRoutes(h) {
		mux.HandleFunc(path, handle)
	}
}

func diagnosticAdminRoutes(h *handler.DiagnosticHandler) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /api/v1/admin/diagnostics/policy":            h.HandleDiagnosticPolicy,
		"PUT /api/v1/admin/diagnostics/policy":            h.HandleSaveDiagnosticPolicy,
		"GET /api/v1/admin/diagnostics/targets":           h.HandleDiagnosticTargets,
		"POST /api/v1/admin/diagnostics/targets":          h.HandleCreateDiagnosticTarget,
		"GET /api/v1/admin/diagnostics/targets/{id}":      h.HandleDiagnosticTarget,
		"PUT /api/v1/admin/diagnostics/targets/{id}":      h.HandleSaveDiagnosticTarget,
		"DELETE /api/v1/admin/diagnostics/targets/{id}":   h.HandleDeleteDiagnosticTarget,
		"GET /api/v1/admin/diagnostics/capabilities":      h.HandleDiagnosticCapabilities,
		"GET /api/v1/admin/diagnostics/jobs":              h.HandleDiagnosticJobs,
		"POST /api/v1/admin/diagnostics/jobs":             h.HandleCreateDiagnosticJob,
		"GET /api/v1/admin/diagnostics/jobs/{id}":         h.HandleDiagnosticJob,
		"POST /api/v1/admin/diagnostics/jobs/{id}/cancel": h.HandleCancelDiagnosticJob,
		"POST /api/v1/admin/diagnostics/jobs/{id}/retry":  h.HandleRetryDiagnosticJob,
		"DELETE /api/v1/admin/diagnostics/jobs/{id}":      h.HandleDeleteDiagnosticJob,
		"GET /api/v1/admin/diagnostics/jobs/{id}/results": h.HandleDiagnosticResults,
	}
}

func diagnosticAgentRoutes(h *handler.DiagnosticHandler) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"POST /api/v1/agent-diagnostics/capability":              h.HandleDiagnosticAdvertise,
		"POST /api/v1/agent-diagnostics/lease":                   h.HandleDiagnosticLease,
		"POST /api/v1/agent-diagnostics/attempts/{id}/start":     h.HandleDiagnosticStart,
		"POST /api/v1/agent-diagnostics/attempts/{id}/heartbeat": h.HandleDiagnosticHeartbeat,
		"POST /api/v1/agent-diagnostics/attempts/{id}/progress":  h.HandleDiagnosticProgress,
		"POST /api/v1/agent-diagnostics/attempts/{id}/complete":  h.HandleDiagnosticComplete,
	}
}
