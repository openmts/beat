package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func (r *Router) registerOperationsRoutes(api *http.ServeMux) {
	r.registerSSHRoutes(api)
	r.registerAlertRoutes(api)
	r.registerTerminalRoutes(api)
}

func (r *Router) registerSSHRoutes(api *http.ServeMux) {
	sshKeyHandler := handler.NewSSHKeyHandler(r.sshKeyStore)
	api.Handle("GET /api/v1/ssh-keys", r.admin(http.HandlerFunc(sshKeyHandler.HandleListSSHKeys)))
	api.Handle("POST /api/v1/ssh-keys", r.admin(http.HandlerFunc(sshKeyHandler.HandleCreateSSHKey)))
	api.Handle("POST /api/v1/ssh-keys/generate", r.admin(http.HandlerFunc(sshKeyHandler.HandleGenerateSSHKey)))
	api.Handle("DELETE /api/v1/ssh-keys/{id}", r.admin(http.HandlerFunc(sshKeyHandler.HandleDeleteSSHKey)))
}

func (r *Router) registerAlertRoutes(api *http.ServeMux) {
	alertHandler := handler.NewAlertHandler(
		r.alertRuleStore, r.alertChannelStore, r.alertEventStore, r.delivery,
	)
	api.Handle("GET /api/v1/alerts/rules", r.admin(http.HandlerFunc(alertHandler.HandleListAlertRules)))
	api.Handle("POST /api/v1/alerts/rules", r.admin(http.HandlerFunc(alertHandler.HandleCreateAlertRule)))
	api.Handle("PUT /api/v1/alerts/rules/{id}", r.admin(http.HandlerFunc(alertHandler.HandleUpdateAlertRule)))
	api.Handle("DELETE /api/v1/alerts/rules/{id}", r.admin(http.HandlerFunc(alertHandler.HandleDeleteAlertRule)))
	api.Handle("GET /api/v1/alerts/channels", r.admin(http.HandlerFunc(alertHandler.HandleListAlertChannels)))
	api.Handle("POST /api/v1/alerts/channels", r.admin(http.HandlerFunc(alertHandler.HandleCreateAlertChannel)))
	api.Handle("PUT /api/v1/alerts/channels/{id}", r.admin(http.HandlerFunc(alertHandler.HandleUpdateAlertChannel)))
	api.Handle("DELETE /api/v1/alerts/channels/{id}", r.admin(http.HandlerFunc(alertHandler.HandleDeleteAlertChannel)))
	api.Handle("POST /api/v1/alerts/channels/{id}/test", r.admin(http.HandlerFunc(alertHandler.HandleTestAlertChannel)))
	api.Handle("GET /api/v1/alerts/events", r.admin(http.HandlerFunc(alertHandler.HandleListAlertEvents)))
	if r.trafficReports == nil {
		return
	}
	reportHandler := handler.NewTrafficReportHandler(r.trafficReports)
	api.Handle("GET /api/v1/alerts/traffic-reports", r.admin(http.HandlerFunc(reportHandler.HandleList)))
	api.Handle("POST /api/v1/alerts/traffic-reports", r.admin(http.HandlerFunc(reportHandler.HandleCreate)))
	api.Handle("PUT /api/v1/alerts/traffic-reports/{id}", r.admin(http.HandlerFunc(reportHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/alerts/traffic-reports/{id}", r.admin(http.HandlerFunc(reportHandler.HandleDelete)))
	api.Handle("POST /api/v1/alerts/traffic-reports/{id}/test", r.admin(http.HandlerFunc(reportHandler.HandleTestRun)))
}

func (r *Router) registerTerminalRoutes(api *http.ServeMux) {
	terminalHandler := handler.NewTerminalHandler(r.terminal)
	metricsHandler := handler.NewMetricsHandler(r.nodeStore, r.mtsStore, r.siteSettingsStore)
	api.Handle("/api/v1/ws/terminal", r.websocketAdmin(http.HandlerFunc(terminalHandler.HandleTerminalWS)))
	api.Handle("POST /api/v1/terminal/execute", r.admin(http.HandlerFunc(terminalHandler.HandleExecuteBatch)))
	api.HandleFunc("/api/v1/ws/metrics", metricsHandler.HandleMetricsWS)
}
