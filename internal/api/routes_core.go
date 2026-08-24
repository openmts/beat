package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/service"
)

func (r *Router) registerAuthAndSettingsRoutes(api *http.ServeMux) {
	settingsHandler := handler.NewSiteSettingsHandler(r.siteSettingsStore)
	if r.security != nil {
		r.registerSessionAuthRoutes(api)
	}
	api.Handle("GET /api/v1/auth/admin", r.admin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler.JSONResponse(w, http.StatusOK, struct{}{})
	})))
	api.HandleFunc("GET /api/v1/settings/site", settingsHandler.HandleGet)
	if r.siteSettingsStore != nil {
		api.Handle("PUT /api/v1/settings/site", r.admin(http.HandlerFunc(settingsHandler.HandleUpdate)))
	}
	if r.maintenance == nil {
		return
	}
	maintenanceHandler := handler.NewMaintenanceHandler(r.maintenance)
	api.Handle("GET /api/v1/settings/maintenance", r.admin(http.HandlerFunc(maintenanceHandler.HandleGet)))
	api.Handle("PUT /api/v1/settings/maintenance", r.admin(http.HandlerFunc(maintenanceHandler.HandleUpdate)))
	api.Handle("POST /api/v1/settings/maintenance/run", r.admin(http.HandlerFunc(maintenanceHandler.HandleRun)))
}

func (r *Router) registerSessionAuthRoutes(api *http.ServeMux) {
	authHandler := handler.NewAdminAuthHandler(r.security)
	api.HandleFunc("GET /api/v1/auth/state", authHandler.HandleState)
	api.Handle("POST /api/v1/auth/bootstrap", requireSameOrigin(http.HandlerFunc(authHandler.HandleBootstrap)))
	api.Handle("POST /api/v1/auth/login", requireSameOrigin(http.HandlerFunc(authHandler.HandleLogin)))
	api.Handle("POST /api/v1/auth/logout", r.admin(http.HandlerFunc(authHandler.HandleLogout)))
	api.Handle("GET /api/v1/auth/session", r.admin(http.HandlerFunc(authHandler.HandleSession)))
	api.Handle("POST /api/v1/auth/reauthenticate", r.admin(http.HandlerFunc(authHandler.HandleReauthenticate)))
	api.Handle("GET /api/v1/admin/users", r.admin(http.HandlerFunc(authHandler.HandleListUsers)))
	api.Handle("POST /api/v1/admin/users", r.admin(http.HandlerFunc(authHandler.HandleCreateUser)))
	api.Handle("PUT /api/v1/admin/users/{id}", r.admin(http.HandlerFunc(authHandler.HandleUpdateUser)))
	api.Handle("DELETE /api/v1/admin/users/{id}", r.admin(http.HandlerFunc(authHandler.HandleDeleteUser)))
	api.Handle("PUT /api/v1/admin/users/me/password", r.admin(http.HandlerFunc(authHandler.HandleChangePassword)))
	api.Handle("POST /api/v1/admin/users/me/totp", r.admin(http.HandlerFunc(authHandler.HandleTOTP)))
	api.Handle("DELETE /api/v1/admin/users/me/totp", r.admin(http.HandlerFunc(authHandler.HandleDisableTOTP)))
	api.Handle("GET /api/v1/admin/sessions", r.admin(http.HandlerFunc(authHandler.HandleListSessions)))
	api.Handle("DELETE /api/v1/admin/sessions/others", r.admin(http.HandlerFunc(authHandler.HandleRevokeOtherSessions)))
	api.Handle("DELETE /api/v1/admin/sessions/{id}", r.admin(http.HandlerFunc(authHandler.HandleRevokeSession)))
	api.Handle("GET /api/v1/admin/audit-events", r.admin(http.HandlerFunc(authHandler.HandleAuditEvents)))
	if r.oidc != nil {
		r.registerOIDCRoutes(api)
	}
	if r.backups != nil {
		r.registerBackupRoutes(api)
	}
}

func (r *Router) registerOIDCRoutes(api *http.ServeMux) {
	oidcHandler := handler.NewOIDCHandler(r.oidc)
	api.HandleFunc("GET /api/v1/auth/oidc/providers", oidcHandler.HandleListPublicProviders)
	api.Handle("POST /api/v1/auth/oidc/providers/{id}/begin",
		requireSameOrigin(http.HandlerFunc(oidcHandler.HandleBeginLogin)))
	api.HandleFunc("GET /api/v1/auth/oidc/callback", oidcHandler.HandleCallback)
	api.Handle("GET /api/v1/admin/oidc/identities/me", r.admin(http.HandlerFunc(oidcHandler.HandleListIdentitiesMe)))
	api.Handle("POST /api/v1/admin/oidc/providers/{id}/link",
		r.admin(http.HandlerFunc(oidcHandler.HandleBeginLink)))
	api.Handle("DELETE /api/v1/admin/oidc/identities/{id}",
		r.admin(http.HandlerFunc(oidcHandler.HandleUnlink)))
	api.Handle("GET /api/v1/admin/oidc/providers", r.ownerAdmin(http.HandlerFunc(oidcHandler.HandleListProviders)))
	api.Handle("POST /api/v1/admin/oidc/providers", r.recentAuthOwnerAdmin(http.HandlerFunc(oidcHandler.HandleCreateProvider)))
	api.Handle("PUT /api/v1/admin/oidc/providers/{id}", r.recentAuthOwnerAdmin(http.HandlerFunc(oidcHandler.HandleUpdateProvider)))
	api.Handle("DELETE /api/v1/admin/oidc/providers/{id}", r.recentAuthOwnerAdmin(http.HandlerFunc(oidcHandler.HandleDeleteProvider)))
}

func (r *Router) registerBackupRoutes(api *http.ServeMux) {
	handler := handler.NewBackupHandler(r.backups, r.security)
	api.Handle("GET /api/v1/admin/backups", r.admin(http.HandlerFunc(handler.HandleList)))
	api.Handle("POST /api/v1/admin/backups", r.admin(http.HandlerFunc(handler.HandleCreate)))
	api.Handle("POST /api/v1/admin/backups/validate", r.admin(http.HandlerFunc(handler.HandleValidate)))
	api.Handle("GET /api/v1/admin/backups/{id}/download", r.admin(http.HandlerFunc(handler.HandleDownload)))
	api.Handle("DELETE /api/v1/admin/backups/{id}", r.admin(http.HandlerFunc(handler.HandleDelete)))
	api.Handle("POST /api/v1/admin/backups/{id}/stage-restore", r.admin(http.HandlerFunc(handler.HandleStage)))
}

func requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !middleware.SameOrigin(request) {
			middleware.WriteAuthError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		next.ServeHTTP(w, request)
	})
}

func (r *Router) registerNodeAndGroupRoutes(api *http.ServeMux) {
	nodeHandler := handler.NewNodeHandler(r.nodeStore, r.mtsStore, r.siteSettingsStore)
	if r.agentTasks != nil {
		channelService := service.NewAgentChannelService(r.agentTasks)
		hub := service.NewAgentHub()
		if r.customMetrics != nil {
			channelService = channelService.WithCustomMetrics(r.customMetrics)
		}
		nodeHandler = nodeHandler.WithTaskEnqueuer(channelService).WithAgentHub(hub)
		if r.customMetrics != nil {
			nodeHandler = nodeHandler.WithCustomMetrics(r.customMetrics)
		}
		channelHandler := handler.NewAgentChannelHandler(channelService, hub)
		api.Handle("GET /api/v1/agents/ws", r.agent(
			http.HandlerFunc(channelHandler.HandleAgentWebSocket), bodyNodeName("name"),
		))
		if r.customMetrics != nil {
			r.registerCustomMetricRoutes(api, channelService, hub)
		}
	}
	groupHandler := handler.NewGroupHandler(r.groupStore)
	api.HandleFunc("GET /api/v1/nodes", nodeHandler.HandleListNodes)
	api.Handle("POST /api/v1/nodes", r.admin(http.HandlerFunc(nodeHandler.HandleCreateManagedNode)))
	api.Handle("GET /api/v1/nodes/manage", r.admin(http.HandlerFunc(nodeHandler.HandleListManagedNodes)))
	api.Handle("PUT /api/v1/nodes/sort", r.admin(http.HandlerFunc(nodeHandler.HandleSortNodes)))
	api.HandleFunc("GET /api/v1/nodes/{id}", nodeHandler.HandleGetNode)
	api.Handle("PUT /api/v1/nodes/{id}", r.admin(http.HandlerFunc(nodeHandler.HandleUpdateNode)))
	api.Handle("DELETE /api/v1/nodes/{id}", r.admin(http.HandlerFunc(nodeHandler.HandleDeleteNode)))
	api.Handle("POST /api/v1/nodes/{id}/token/rotate", r.admin(http.HandlerFunc(nodeHandler.HandleRotateAgentToken)))
	api.Handle("POST /api/v1/nodes/{id}/token/revoke", r.admin(http.HandlerFunc(nodeHandler.HandleRevokeAgentToken)))
	api.Handle("GET /api/v1/nodes/{id}/install", r.admin(http.HandlerFunc(nodeHandler.HandleAgentInstallConfig)))
	api.Handle("POST /api/v1/nodes/report", r.agent(
		http.HandlerFunc(nodeHandler.HandleNodeReport), bodyNodeName("name"),
	))
	api.HandleFunc("GET /api/v1/nodes/{id}/metrics", nodeHandler.HandleGetNodeMetrics)
	api.HandleFunc("GET /api/v1/nodes/{id}/custom-metrics", nodeHandler.HandleGetNodeCustomMetrics)
	api.HandleFunc("GET /api/v1/groups", groupHandler.HandleListGroups)
	api.Handle("POST /api/v1/groups", r.admin(http.HandlerFunc(groupHandler.HandleCreateGroup)))
	api.Handle("PUT /api/v1/groups/{id}", r.admin(http.HandlerFunc(groupHandler.HandleUpdateGroup)))
	api.Handle("DELETE /api/v1/groups/{id}", r.admin(http.HandlerFunc(groupHandler.HandleDeleteGroup)))
	api.Handle("PUT /api/v1/groups/sort", r.admin(http.HandlerFunc(groupHandler.HandleUpdateSortOrder)))
	api.Handle("PUT /api/v1/groups/{id}/default", r.admin(http.HandlerFunc(groupHandler.HandleSetDefaultGroup)))
}

func (r *Router) registerCustomMetricRoutes(
	api *http.ServeMux,
	channelService *service.AgentChannelService,
	hub *service.AgentHub,
) {
	dispatcher := service.NewCustomMetricDispatchService(channelService, hub)
	customMetricHandler := handler.NewCustomMetricHandler(r.customMetrics, dispatcher)
	api.Handle("GET /api/v1/custom-metrics", r.admin(http.HandlerFunc(customMetricHandler.HandleList)))
	api.Handle("POST /api/v1/custom-metrics", r.admin(http.HandlerFunc(customMetricHandler.HandleCreate)))
	api.Handle("PUT /api/v1/custom-metrics/{id}", r.admin(http.HandlerFunc(customMetricHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/custom-metrics/{id}", r.admin(http.HandlerFunc(customMetricHandler.HandleDelete)))
	api.Handle("GET /api/v1/custom-metrics/{id}/nodes", r.admin(http.HandlerFunc(customMetricHandler.HandleListMetricNodes)))
	api.Handle("PUT /api/v1/custom-metrics/{id}/nodes", r.admin(http.HandlerFunc(customMetricHandler.HandleSetMetricNodes)))
}
