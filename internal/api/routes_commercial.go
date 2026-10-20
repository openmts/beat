package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

func WithRemoteOperations(operations handler.RemoteOperations) RouterOption {
	return func(router *Router) { router.remoteOperations = operations }
}

func WithDiagnostics(service handler.Diagnostics) RouterOption {
	return func(router *Router) { router.diagnostics = service }
}

func WithAlertEvaluation(config handler.AlertEvaluationHandlerConfig) RouterOption {
	return func(router *Router) {
		if config.Rules != nil {
			router.alertEvaluation = &config
		}
	}
}

func (router *Router) registerCommercialRoutes(mux *http.ServeMux) {
	RegisterOperationRoutes(mux, OperationRouteConfig{Operations: router.remoteOperations, ProtectAdmin: router.admin})
	RegisterDiagnosticRoutes(mux, DiagnosticRouteConfig{Diagnostics: router.diagnostics, ProtectAdmin: router.admin})
	if router.alertEvaluation != nil {
		RegisterAlertEvaluationRoutes(mux, AlertEvaluationRouteConfig{
			Handler: *router.alertEvaluation, ProtectAdmin: router.admin,
			ProtectRulesRead: router.serviceTokenPrivateRoute(model.ServiceScopeAlertsRead, router.admin),
		})
	}
}
