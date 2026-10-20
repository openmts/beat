package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

func (r *Router) registerAuthAndSettingsRoutes(api *http.ServeMux) {
	settingsHandler := handler.NewSiteSettingsHandler(r.siteSettingsStore)
	// 未注入遥测存储时保持关闭；把类型化 nil 传进接口会造成空指针解引用。
	if r.publicTelemetry != nil {
		settingsHandler = settingsHandler.WithPublicTelemetry(r.publicTelemetry)
	}
	if r.security != nil {
		r.registerSessionAuthRoutes(api)
	}
	api.Handle("GET /api/v1/auth/admin", r.admin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler.JSONResponse(w, http.StatusOK, struct{}{})
	})))
	api.HandleFunc("GET /api/v1/settings/site", settingsHandler.HandleGet)
	// 部署访问策略是环境配置，只有 owner/admin 能看到汇总状态，且永远不下发来源列表。
	api.Handle("GET /api/v1/admin/settings/deployment", r.admin(http.HandlerFunc(
		func(w http.ResponseWriter, request *http.Request) {
			handler.JSONResponse(w, http.StatusOK, r.deploymentAccess.Status())
		})))
	if r.siteSettingsStore != nil {
		api.Handle("PUT /api/v1/settings/site", r.metadataAdmin(http.HandlerFunc(settingsHandler.HandleUpdate), false))
	}
	r.registerPublicTelemetryRoutes(api)
	r.registerTrafficCalibrationRoutes(api)
	r.registerMetricErasureRoutes(api)
	if r.applicationGc != nil {
		retentionHandler := handler.NewApplicationRetentionHandler(r.applicationGc)
		api.Handle("GET /api/v1/settings/application-retention",
			r.admin(http.HandlerFunc(retentionHandler.HandleGet)))
		api.Handle("PUT /api/v1/settings/application-retention",
			r.admin(http.HandlerFunc(retentionHandler.HandleUpdate)))
		api.Handle("POST /api/v1/settings/application-retention/run",
			r.admin(http.HandlerFunc(retentionHandler.HandleRun)))
	}
	r.registerAgentRetentionRoutes(api)
	if r.maintenance == nil {
		return
	}
	maintenanceHandler := handler.NewMaintenanceHandler(r.maintenance)
	api.Handle("GET /api/v1/settings/maintenance", r.admin(http.HandlerFunc(maintenanceHandler.HandleGet)))
	api.Handle("PUT /api/v1/settings/maintenance", r.admin(http.HandlerFunc(maintenanceHandler.HandleUpdate)))
	api.Handle("POST /api/v1/settings/maintenance/run", r.admin(http.HandlerFunc(maintenanceHandler.HandleRun)))
}

// registerAgentRetentionRoutes 挂发布对象保留期的后台配置：读取、保存、回到环境缺省与手动清理。
func (r *Router) registerAgentRetentionRoutes(api *http.ServeMux) {
	if r.agentRetention == nil {
		return
	}
	retentionHandler := handler.NewAgentRetentionHandler(r.agentRetention)
	api.Handle("GET /api/v1/settings/agent-release-retention", r.admin(http.HandlerFunc(retentionHandler.HandleGet)))
	api.Handle("PUT /api/v1/settings/agent-release-retention", r.admin(http.HandlerFunc(retentionHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/settings/agent-release-retention", r.admin(http.HandlerFunc(retentionHandler.HandleReset)))
	api.Handle("POST /api/v1/settings/agent-release-retention/run", r.admin(http.HandlerFunc(retentionHandler.HandleRun)))
}

func (r *Router) registerSessionAuthRoutes(api *http.ServeMux) {
	authHandler := handler.NewAdminAuthHandler(r.security)
	api.HandleFunc("GET /api/v1/auth/state", authHandler.HandleState)
	api.Handle("POST /api/v1/auth/bootstrap", requireSameOrigin(http.HandlerFunc(authHandler.HandleBootstrap)))
	api.Handle("POST /api/v1/auth/login", requireSameOrigin(http.HandlerFunc(authHandler.HandleLogin)))
	api.Handle("POST /api/v1/auth/logout",
		r.admin(r.runtimeSessionCleanup(http.HandlerFunc(authHandler.HandleLogout), runtimeCurrentSession)))
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
	api.Handle("DELETE /api/v1/admin/sessions/others",
		r.admin(r.runtimeSessionCleanup(http.HandlerFunc(authHandler.HandleRevokeOtherSessions), r.runtimeOtherSessions)))
	api.Handle("DELETE /api/v1/admin/sessions/{id}",
		r.admin(r.runtimeSessionCleanup(http.HandlerFunc(authHandler.HandleRevokeSession), runtimePathSession)))
	api.Handle("GET /api/v1/admin/audit-events",
		r.serviceTokenPrivateRoute(model.ServiceScopeAuditRead, r.admin)(http.HandlerFunc(authHandler.HandleAuditEvents)))
	if r.oidc != nil {
		r.registerOIDCRoutes(api)
	}
	if r.themeService != nil {
		r.registerThemeRoutes(api)
	}
	if r.backups != nil {
		r.registerBackupRoutes(api)
	}
}

func (r *Router) registerThemeRoutes(api *http.ServeMux) {
	themeHandler := handler.NewThemeHandler(r.themeService, r.themeMarkets)
	api.HandleFunc("GET /api/v1/themes/active", themeHandler.HandleActive)
	api.Handle("GET /api/v1/admin/themes", r.ownerAdmin(http.HandlerFunc(themeHandler.HandleList)))
	api.Handle("POST /api/v1/admin/themes/upload", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleUpload)))
	api.Handle("GET /api/v1/admin/themes/{id}/export", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleExport)))
	api.Handle("GET /api/v1/admin/themes/{id}/preview", r.ownerAdmin(http.HandlerFunc(themeHandler.HandlePreview)))
	api.Handle("PUT /api/v1/admin/themes/{id}/configuration", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleConfiguration)))
	api.Handle("POST /api/v1/admin/themes/{id}/activate", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleActivate)))
	api.Handle("POST /api/v1/admin/themes/rollback", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleRollback)))
	api.Handle("POST /api/v1/admin/themes/built-in", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleBuiltIn)))
	api.Handle("DELETE /api/v1/admin/themes/{id}", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleDelete)))
	api.Handle("GET /api/v1/admin/theme-markets", r.ownerAdmin(http.HandlerFunc(themeHandler.HandleListMarkets)))
	api.Handle("POST /api/v1/admin/theme-markets", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleCreateMarket)))
	api.Handle("PUT /api/v1/admin/theme-markets/{id}", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleUpdateMarket)))
	api.Handle("DELETE /api/v1/admin/theme-markets/{id}", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleDeleteMarket)))
	api.Handle("GET /api/v1/admin/theme-markets/{id}/catalog", r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleRefreshCatalog)))
	api.Handle("POST /api/v1/admin/theme-markets/{id}/themes/{theme_id}/install",
		r.recentAuthOwnerAdmin(http.HandlerFunc(themeHandler.HandleInstallFromMarket)))
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
	uploadHandler := handler.NewBackupUploadHandler(r.backups, r.security)
	handler := handler.NewBackupHandler(r.backups, r.security)
	api.Handle("GET /api/v1/admin/backups",
		r.serviceTokenPrivateRoute(model.ServiceScopeBackupsReadMetadata, r.admin)(http.HandlerFunc(handler.HandleList)))
	api.Handle("POST /api/v1/admin/backups",
		r.serviceTokenPrivateRoute(model.ServiceScopeBackupsCreate, r.admin)(http.HandlerFunc(handler.HandleCreate)))
	api.Handle("GET /api/v1/admin/backups/quarantine",
		r.serviceTokenPrivateRoute(model.ServiceScopeBackupsReadMetadata, r.admin)(http.HandlerFunc(handler.HandleQuarantine)))
	api.Handle("POST /api/v1/admin/backups/validate", r.admin(http.HandlerFunc(handler.HandleValidate)))
	api.Handle("GET /api/v1/admin/backups/{id}/download",
		r.serviceTokenPrivateRoute(model.ServiceScopeBackupsDownload, r.admin)(http.HandlerFunc(handler.HandleDownload)))
	api.Handle("DELETE /api/v1/admin/backups/{id}", r.admin(http.HandlerFunc(handler.HandleDelete)))
	api.Handle("POST /api/v1/admin/backups/{id}/stage-restore", r.admin(http.HandlerFunc(handler.HandleStage)))
	api.Handle("GET /api/v1/admin/backups/uploads",
		r.ownerAdmin(http.HandlerFunc(uploadHandler.HandleList)))
	api.Handle("POST /api/v1/admin/backups/uploads",
		r.recentAuthOwnerAdmin(http.HandlerFunc(uploadHandler.HandleStart)))
	// 会话状态带 /status 后缀：GET /api/v1/admin/backups/{id}/download 与
	// /backups/uploads/{session} 在 net/http 路由里互相歧义，会造成启动期 panic。
	api.Handle("GET /api/v1/admin/backups/uploads/{session}/status",
		r.ownerAdmin(http.HandlerFunc(uploadHandler.HandleStatus)))
	api.Handle("PUT /api/v1/admin/backups/uploads/{session}/chunks/{index}",
		r.ownerAdmin(http.HandlerFunc(uploadHandler.HandleChunk)))
	api.Handle("POST /api/v1/admin/backups/uploads/{session}/finalize",
		r.recentAuthOwnerAdmin(http.HandlerFunc(uploadHandler.HandleFinalize)))
	api.Handle("DELETE /api/v1/admin/backups/uploads/{session}",
		r.ownerAdmin(http.HandlerFunc(uploadHandler.HandleCancel)))
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
	nodeHandler := handler.NewNodeHandler(r.nodeStore, r.mtsStore, r.siteSettingsStore).WithErasureStore(r.metricErasure).WithPublicSnapshot(r.metricsHandler)
	nodeHandler = nodeHandler.WithMetricCatalog(r.metricCatalog).WithLocationViews(r.locationViews).WithLifecycleViews(r.lifecycleViews).WithAccelerators(r.accelerators)
	nodeHandler = nodeHandler.WithMetricQueryBudget(r.metricQueries)
	nodeHandler = nodeHandler.WithTrafficResolver(r.trafficAccountingResolver())
	if r.agentTasks != nil {
		channelService := service.NewAgentChannelService(r.agentTasks)
		hub := service.NewAgentHub()
		r.nodeStore.WithCredentialInvalidator(hub.DisconnectCredential)
		if r.customMetrics != nil {
			channelService = channelService.WithCustomMetrics(r.customMetrics)
		}
		nodeHandler = nodeHandler.WithTaskEnqueuer(channelService).WithAgentHub(hub)
		if r.customMetrics != nil {
			nodeHandler = nodeHandler.WithCustomMetrics(r.customMetrics)
		}
		channelHandler := handler.NewAgentChannelHandler(channelService, hub).WithIdentityVerifier(r.nodeStore.VerifyAgentIdentity)
		api.Handle("GET /api/v1/agents/ws", r.agent(
			http.HandlerFunc(channelHandler.HandleAgentWebSocket), bodyNodeName("name"),
		))
		if r.customMetrics != nil {
			r.registerCustomMetricRoutes(api, channelService, hub)
		}
	}
	groupHandler := handler.NewGroupHandler(r.groupStore)
	api.HandleFunc("GET /api/v1/nodes", nodeHandler.HandleListNodes)
	api.Handle("POST /api/v1/nodes", r.metadataAdmin(http.HandlerFunc(nodeHandler.HandleCreateManagedNode), true))
	api.Handle("GET /api/v1/nodes/manage",
		r.serviceTokenPrivateRoute(model.ServiceScopeNodesRead, r.admin)(http.HandlerFunc(nodeHandler.HandleListManagedNodes)))
	api.Handle("PUT /api/v1/nodes/sort", r.metadataAdmin(http.HandlerFunc(nodeHandler.HandleSortNodes), false))
	api.HandleFunc("GET /api/v1/nodes/{id}", nodeHandler.HandleGetNode)
	api.Handle("PUT /api/v1/nodes/{id}", r.serviceTokenResourceRoute(
		model.ServiceScopeNodesWritePresentation, model.ServiceResourceNode, middleware.ServicePathResource("id"),
		func(next http.Handler) http.Handler { return r.metadataAdmin(next, false) },
	)(http.HandlerFunc(nodeHandler.HandleUpdateNode)))
	api.Handle("DELETE /api/v1/nodes/{id}", r.metadataOwner(http.HandlerFunc(nodeHandler.HandleDeleteNode)))
	api.Handle("POST /api/v1/nodes/{id}/token/rotate", r.metadataAdmin(http.HandlerFunc(nodeHandler.HandleRotateAgentToken), true))
	api.Handle("POST /api/v1/nodes/{id}/token/revoke", r.metadataAdmin(http.HandlerFunc(nodeHandler.HandleRevokeAgentToken), true))
	api.Handle("GET /api/v1/nodes/{id}/install", r.admin(http.HandlerFunc(nodeHandler.HandleAgentInstallConfig)))
	api.Handle("POST /api/v1/nodes/report", r.agentReport(http.HandlerFunc(nodeHandler.HandleNodeReport)))
	api.Handle("GET /api/v1/nodes/{id}/metrics", r.serviceTokenResourceRoute(
		model.ServiceScopeMetricsRead, model.ServiceResourceNode, middleware.ServicePathResource("id"), nil,
	)(http.HandlerFunc(nodeHandler.HandleGetNodeMetrics)))
	api.HandleFunc("GET /api/v1/nodes/{id}/custom-metrics", nodeHandler.HandleGetNodeCustomMetrics)
	api.HandleFunc("GET /api/v1/groups", groupHandler.HandleListGroups)
	api.Handle("POST /api/v1/groups", r.metadataAdmin(http.HandlerFunc(groupHandler.HandleCreateGroup), false))
	api.Handle("PUT /api/v1/groups/{id}", r.metadataAdmin(http.HandlerFunc(groupHandler.HandleUpdateGroup), false))
	api.Handle("DELETE /api/v1/groups/{id}", r.metadataAdmin(http.HandlerFunc(groupHandler.HandleDeleteGroup), false))
	api.Handle("PUT /api/v1/groups/sort", r.metadataAdmin(http.HandlerFunc(groupHandler.HandleUpdateSortOrder), false))
	api.Handle("PUT /api/v1/groups/{id}/default", r.metadataAdmin(http.HandlerFunc(groupHandler.HandleSetDefaultGroup), false))
}

func (r *Router) registerCustomMetricRoutes(
	api *http.ServeMux,
	channelService *service.AgentChannelService,
	hub *service.AgentHub,
) {
	dispatcher := service.NewCustomMetricDispatchService(channelService, hub)
	r.metricDispatcher = dispatcher
	customMetricHandler := handler.NewCustomMetricHandler(r.customMetrics, dispatcher)
	if r.mtsStore != nil {
		customMetricHandler = customMetricHandler.WithCustomSources(r.mtsStore)
	}
	api.Handle("GET /api/v1/custom-metrics", r.admin(http.HandlerFunc(customMetricHandler.HandleList)))
	api.Handle("POST /api/v1/custom-metrics", r.admin(http.HandlerFunc(customMetricHandler.HandleCreate)))
	api.Handle("PUT /api/v1/custom-metrics/{id}", r.admin(http.HandlerFunc(customMetricHandler.HandleUpdate)))
	api.Handle("DELETE /api/v1/custom-metrics/{id}", r.admin(http.HandlerFunc(customMetricHandler.HandleDelete)))
	api.Handle("GET /api/v1/custom-metrics/{id}/nodes", r.admin(http.HandlerFunc(customMetricHandler.HandleListMetricNodes)))
	api.Handle("PUT /api/v1/custom-metrics/{id}/nodes", r.admin(http.HandlerFunc(customMetricHandler.HandleSetMetricNodes)))
	api.Handle("GET /api/v1/custom-metrics/{id}/health", r.admin(http.HandlerFunc(customMetricHandler.HandleHealth)))
	api.Handle("POST /api/v1/custom-metrics/{id}/probe", r.admin(http.HandlerFunc(customMetricHandler.HandleProbe)))
	api.Handle("GET /api/v1/custom-metrics/{id}/probe/{task_id}", r.admin(http.HandlerFunc(customMetricHandler.HandleProbeResult)))
}

// effectiveAPIOrigins 在未注入部署策略时保留历史通配行为，供 CORS 与公共交互端点共用。
func (r *Router) effectiveAPIOrigins() deploymentaccess.OriginPolicy {
	origins := r.deploymentAccess.APIOrigins()
	if origins.Mode() != deploymentaccess.OriginModeSameOrigin || r.deploymentAccess.PublicBaseURL() != "" {
		return origins
	}
	fallback, err := deploymentaccess.ParseOrigins("*")
	if err != nil {
		return origins
	}
	return fallback
}

// registerPublicTelemetryRoutes 只注册有界的匿名交互端点；策略读写属于 owner。
func (r *Router) registerPublicTelemetryRoutes(api *http.ServeMux) {
	if r.publicTelemetry == nil {
		return
	}
	policyHandler := handler.NewPublicTelemetryHandler(r.publicTelemetry)
	api.Handle("GET /api/v1/admin/settings/public-telemetry", r.ownerAdmin(http.HandlerFunc(policyHandler.HandleGet)))
	api.Handle("PUT /api/v1/admin/settings/public-telemetry",
		r.recentAuthOwnerAdmin(http.HandlerFunc(policyHandler.HandleUpdate)))
	if r.mtsStore == nil {
		return
	}
	interactions := handler.NewPublicInteractionsHandler(r.publicTelemetry, r.mtsStore, r.effectiveAPIOrigins())
	api.HandleFunc("POST /api/v1/public/interactions", interactions.HandlePost)
}
