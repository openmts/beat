package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

type AlertEvaluationRouteConfig struct {
	Handler      handler.AlertEvaluationHandlerConfig
	ProtectAdmin func(http.Handler) http.Handler
	// ProtectRulesRead 可放宽规则列表读取（服务令牌 alerts:read）；为空时沿用 ProtectAdmin。
	ProtectRulesRead func(http.Handler) http.Handler
}

const alertEvaluationRulesReadPath = "GET /api/v1/alerts/rules"

// RegisterAlertEvaluationRoutes 由v12开关替换旧rule路由，不能与旧rule路由重复注册。
func RegisterAlertEvaluationRoutes(mux *http.ServeMux, config AlertEvaluationRouteConfig) {
	if mux == nil || config.Handler.Rules == nil || config.Handler.States == nil || config.ProtectAdmin == nil {
		return
	}
	h := handler.NewAlertEvaluationHandler(config.Handler)
	for path, handle := range map[string]http.HandlerFunc{
		"GET /api/v1/alerts/rules":                           h.HandleListEvaluationRules,
		"POST /api/v1/alerts/rules":                          h.HandleSaveEvaluationRule,
		"GET /api/v1/alerts/rules/{id}":                      h.HandleGetEvaluationRule,
		"PUT /api/v1/alerts/rules/{id}":                      h.HandleSaveEvaluationRule,
		"DELETE /api/v1/alerts/rules/{id}":                   h.HandleDeleteEvaluationRule,
		"POST /api/v1/alerts/rules/preview":                  h.HandlePreviewEvaluationRule,
		"GET /api/v1/alerts/evaluations":                     h.HandleListEvaluations,
		"GET /api/v1/alerts/evaluations/{rule_id}/{node_id}": h.HandleGetEvaluation,
		"GET /api/v1/alerts/evaluation-events":               h.HandleListEvaluationEvents,
	} {
		protect := config.ProtectAdmin
		if protect == nil || (path == alertEvaluationRulesReadPath && config.ProtectRulesRead != nil) {
			protect = config.ProtectRulesRead
		}
		mux.Handle(path, protect(handle))
	}
}
