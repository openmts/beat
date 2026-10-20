package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

func (r *Router) registerOperationsRoutes(api *http.ServeMux) {
	r.registerSSHRoutes(api)
	r.registerAlertRoutes(api)
	r.registerFleetSummaryRoutes(api)
	r.registerTerminalRoutes(api)
	if r.runtimeStatus != nil {
		api.Handle("GET /api/v1/admin/runtime",
			r.serviceTokenPrivateRoute(model.ServiceScopeHealthRead, r.ownerAdmin)(r.runtimeStatus))
	}
}

func (r *Router) registerSSHRoutes(api *http.ServeMux) {
	sshKeyHandler := handler.NewSSHKeyHandler(r.sshKeyStore)
	api.Handle("GET /api/v1/ssh-keys", r.admin(http.HandlerFunc(sshKeyHandler.HandleListSSHKeys)))
	api.Handle("POST /api/v1/ssh-keys", r.metadataAdmin(http.HandlerFunc(sshKeyHandler.HandleCreateSSHKey), true))
	api.Handle("POST /api/v1/ssh-keys/generate", r.metadataAdmin(http.HandlerFunc(sshKeyHandler.HandleGenerateSSHKey), true))
	api.Handle("GET /api/v1/ssh-keys/{id}", r.metadataAdmin(http.HandlerFunc(sshKeyHandler.HandleGetSSHKey), true))
	api.Handle("DELETE /api/v1/ssh-keys/{id}", r.metadataAdmin(http.HandlerFunc(sshKeyHandler.HandleDeleteSSHKey), true))
}

func (r *Router) registerAlertRoutes(api *http.ServeMux) {
	alertHandler := handler.NewAlertHandler(
		r.alertRuleStore, r.alertChannelStore, r.alertEventStore, r.nodeStore, r.delivery,
	)
	alertHandler.SetNotifications(r.alertNotifications)
	if r.alertEvaluation == nil {
		api.Handle("GET /api/v1/alerts/rules",
			r.serviceTokenPrivateRoute(model.ServiceScopeAlertsRead, r.admin)(http.HandlerFunc(alertHandler.HandleListAlertRules)))
		api.Handle("POST /api/v1/alerts/rules", r.admin(http.HandlerFunc(alertHandler.HandleCreateAlertRule)))
		api.Handle("PUT /api/v1/alerts/rules/{id}", r.admin(http.HandlerFunc(alertHandler.HandleUpdateAlertRule)))
		api.Handle("DELETE /api/v1/alerts/rules/{id}", r.admin(http.HandlerFunc(alertHandler.HandleDeleteAlertRule)))

	}

	api.Handle("GET /api/v1/alerts/events",
		r.serviceTokenPrivateRoute(model.ServiceScopeAlertsRead, r.admin)(http.HandlerFunc(alertHandler.HandleListAlertEvents)))
	api.Handle("PUT /api/v1/alerts/events/{id}/acknowledge", r.admin(http.HandlerFunc(alertHandler.HandleAcknowledgeAlertEvent)))
	api.Handle("PUT /api/v1/alerts/events/{id}/resolve", r.admin(http.HandlerFunc(alertHandler.HandleResolveAlertEvent)))
	if r.trafficReports == nil {
		return
	}
	reportHandler := handler.NewTrafficReportHandler(r.trafficReports)
	api.Handle("GET /api/v1/alerts/traffic-reports",
		r.serviceTokenPrivateRoute(model.ServiceScopeTrafficReportsRead, r.admin)(http.HandlerFunc(reportHandler.HandleList)))
	api.Handle("POST /api/v1/alerts/traffic-reports", r.admin(http.HandlerFunc(reportHandler.HandleCreate)))
	api.Handle("PUT /api/v1/alerts/traffic-reports/{id}", r.admin(http.HandlerFunc(reportHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/alerts/traffic-reports/{id}", r.admin(http.HandlerFunc(reportHandler.HandleDelete)))
	api.Handle("POST /api/v1/alerts/traffic-reports/{id}/test",
		r.serviceTokenPrivateRoute(model.ServiceScopeTrafficReportsRunTest, r.admin)(http.HandlerFunc(reportHandler.HandleTestRun)))
}

// registerFleetSummaryRoutes 只注册管理员可用的舰队状态摘要读写；不开放服务账号范围。
func (r *Router) registerFleetSummaryRoutes(api *http.ServeMux) {
	if r.fleetSummaries == nil {
		return
	}
	summaryHandler := handler.NewFleetSummaryHandler(r.fleetSummaries)
	api.Handle("GET /api/v1/alerts/fleet-summaries", r.admin(http.HandlerFunc(summaryHandler.HandleList)))
	api.Handle("POST /api/v1/alerts/fleet-summaries", r.admin(http.HandlerFunc(summaryHandler.HandleCreate)))
	api.Handle("PUT /api/v1/alerts/fleet-summaries/{id}", r.admin(http.HandlerFunc(summaryHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/alerts/fleet-summaries/{id}", r.admin(http.HandlerFunc(summaryHandler.HandleDelete)))
	api.Handle("POST /api/v1/alerts/fleet-summaries/{id}/test-run", r.admin(http.HandlerFunc(summaryHandler.HandleTestRun)))
}

func (r *Router) registerTerminalRoutes(api *http.ServeMux) {
	terminalHandler := handler.NewTerminalHandler(r.terminal)
	api.Handle("/api/v1/ws/terminal", r.websocketAdmin(http.HandlerFunc(terminalHandler.HandleTerminalWS)))
	api.Handle("POST /api/v1/terminal/execute", r.admin(r.guardedAdminOperation(http.HandlerFunc(terminalHandler.HandleExecuteBatch))))
	api.HandleFunc("/api/v1/ws/metrics", r.metricsHandler.HandleMetricsWS)
}
