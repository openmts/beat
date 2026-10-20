package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

type OperationRouteConfig struct {
	Operations   handler.RemoteOperations
	ProtectAdmin func(http.Handler) http.Handler
}

// RegisterOperationRoutes 在 Server 显式提供已初始化服务及会话保护时注册私有 API。
func RegisterOperationRoutes(mux *http.ServeMux, config OperationRouteConfig) {
	if config.Operations == nil || config.ProtectAdmin == nil {
		return
	}
	registerRemoteTransferRoutes(mux, config)
	h := handler.NewRemoteOperationsHandler(config.Operations)
	for path, handle := range operationAdminRoutes(h) {
		mux.Handle(path, config.ProtectAdmin(handle))
	}
	for path, handle := range map[string]http.HandlerFunc{
		"POST /api/v1/agent-operations/lease":                   h.HandleOperationLease,
		"POST /api/v1/agent-operations/attempts/{id}/start":     h.HandleOperationStart,
		"POST /api/v1/agent-operations/attempts/{id}/heartbeat": h.HandleOperationHeartbeat,
		"POST /api/v1/agent-operations/attempts/{id}/complete":  h.HandleOperationComplete,
	} {
		mux.Handle(path, handle)
	}
}

func operationAdminRoutes(h *handler.RemoteOperationsHandler) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /api/v1/admin/operations/status":                                h.HandleOperationStatus,
		"GET /api/v1/admin/operations/tasks/{id}/content":                    h.HandleOperationTaskContent,
		"POST /api/v1/admin/operations/nodes/{id}/force-delete":              h.HandleForceDeleteOperationNode,
		"GET /api/v1/admin/operations/executable-policies":                   h.HandleExecutableOperationPolicies,
		"GET /api/v1/admin/operations/policies":                              h.HandleOperationPolicies,
		"POST /api/v1/admin/operations/policies":                             h.HandleSaveOperationPolicy,
		"GET /api/v1/admin/operations/policies/{id}":                         h.HandleOperationPolicy,
		"PUT /api/v1/admin/operations/policies/{id}":                         h.HandleSaveOperationPolicy,
		"POST /api/v1/admin/operations/policies/{id}/disable":                h.HandleDisableOperationPolicy,
		"GET /api/v1/admin/operations/snippets":                              h.HandleOperationSnippets,
		"POST /api/v1/admin/operations/snippets":                             h.HandleSaveOperationSnippet,
		"GET /api/v1/admin/operations/snippets/{id}":                         h.HandleOperationSnippetContent,
		"PUT /api/v1/admin/operations/snippets/{id}":                         h.HandleSaveOperationSnippet,
		"DELETE /api/v1/admin/operations/snippets/{id}":                      h.HandleDeleteOperationSnippet,
		"POST /api/v1/admin/operations/snippets/{id}/approve":                h.HandleApproveOperationSnippet,
		"GET /api/v1/admin/operations/tasks":                                 h.HandleOperationTasks,
		"POST /api/v1/admin/operations/tasks":                                h.HandleCreateOperationTask,
		"GET /api/v1/admin/operations/tasks/{id}":                            h.HandleOperationTask,
		"POST /api/v1/admin/operations/tasks/{id}/cancel":                    h.HandleCancelOperationTask,
		"POST /api/v1/admin/operations/tasks/{id}/targets/{target_id}/retry": h.HandleRetryOperationTarget,
		"GET /api/v1/admin/operations/tasks/{id}/targets/{target_id}/output": h.HandleOperationTargetOutput,
	}
}
