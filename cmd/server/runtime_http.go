package main

import (
	"net/http"
	"os"

	"github.com/beat/backend/internal/alerter"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/runtimediag"
	"github.com/beat/backend/internal/store"
)

type httpHandlerConfig struct {
	dataDir          string
	staticDir        string
	stores           *serverStores
	services         *serverServices
	trustedProxies   middleware.TrustedProxies
	runtime          *runtimeState
	deploymentAccess deploymentaccess.Config
}

// metricErasureSignal 让 202 响应立刻唤醒擦除 worker，而不是等下一个轮询间隔。
func metricErasureSignal(config httpHandlerConfig) func() {
	if config.services == nil || config.services.metricErasure == nil {
		return nil
	}
	return config.services.metricErasure.Signal
}

func buildHTTPHandler(config httpHandlerConfig) (http.Handler, *alerter.Alerter, *metricsfanout.Service, error) {
	metrics := observability.NewRegistry()
	if config.runtime != nil {
		config.runtime.metrics.Store(metrics)
	}
	registerServiceTokenMetrics(metrics, config.services)
	registerEnrollmentMetrics(metrics, config.stores)
	runtimeDiagnosticsMetrics(metrics, runtimeDiagnosticsService(config.services))
	limiter, err := initializeIngress(os.Getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	if config.runtime != nil {
		config.runtime.ingress.Store(limiter)
	}
	metrics.SetIngressSnapshotSource(limiter.Snapshot)
	operations := newOperability(config, metrics)
	router := api.NewRouter(config.stores.nodes, config.stores.groups, config.stores.sshKeys, config.stores.alertRules,
		config.stores.alertChannels, config.stores.alertEvents, config.stores.mts,
		metricDashboardOption(config.stores),
		api.WithRuntimeStatus(http.HandlerFunc(config.runtime.handleStatus)),
		api.WithIngressLimiter(limiter),
		api.WithAdminToken(os.Getenv("BEAT_ADMIN_TOKEN")),
		api.WithAgentAuthentication(os.Getenv("BEAT_AGENT_TOKEN")),
		api.WithTerminalOperations(config.services.terminal), api.WithNetworkTaskStore(config.stores.networkTasks),
		runtimeDiagnosticsRouterOption(config.services),
		api.WithMigrationOverview(newMigrationOverviewSource(config.stores, config.dataDir)),
		api.WithSiteSettingsStore(config.stores.siteSettings), api.WithPublicTelemetryStore(config.stores.publicTelemetry),
		api.WithTrafficCalibrationStore(config.stores.trafficCalibration),
		api.WithMetricErasureStore(config.stores.metricErasure, metricErasureSignal(config)),
		api.WithNotificationQueue(config.services.delivery),
		api.WithAlertNotifications(config.services.alertNotifications),
		api.WithRemoteOperations(config.services.operations),
		diagnosticRouterOption(config.services),
		nodeLifecycleRouterOption(config.services),
		acceleratorRouterOption(config.services),
		agentRolloutRouterOption(config.services),
		servicePrincipalRouterOption(config.services),
		serviceTokenAccessRouterOption(config.services, metrics, config.stores.nodes),
		alertEvaluationOption(config.services),
		api.WithTrafficReportService(config.services.trafficReports), api.WithMaintenanceService(config.services.maintenance),
		agentRetentionRouterOption(config.services),
		api.WithFleetSummaryService(config.services.fleetSummaries),
		api.WithApplicationRetentionService(config.services.applicationGc),
		api.WithAdminSecurity(config.services.security), api.WithBackupService(config.services.backups),
		api.WithApplicationSecrets(config.stores.secrets),
		api.WithAgentTaskStore(config.stores.agentTasks),
		api.WithCustomMetricStore(config.stores.customMetrics),
		metricCatalogOption(config.stores),
		api.WithOIDC(config.services.oidc),
		api.WithThemes(config.services.themes, config.services.themeMarkets),
		api.WithAgentEnrollmentStore(store.NewAgentEnrollmentStore(config.stores.sqlite.DB)),
		api.WithAgentEnrollmentMetrics(metrics),
		deploymentAccessRouterOption(config.deploymentAccess))
	fanout, fanoutErr := metricsfanout.New(metricsfanout.Config{Build: config.runtime.publicBuilder(router), ClientIP: middleware.ClientIP,
		AllowOrigin: handler.MetricsOriginPolicy(config.deploymentAccess.WSOrigins()), Observer: config.runtime.observeFanout})
	if fanoutErr != nil {
		return nil, nil, nil, fanoutErr
	}
	config.runtime.fanout.Store(fanout)
	router.SetMetricsStream(fanout)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", router.ServeHandler())
	// 主题资源前缀不落在 /api/v1 下，要在最外层 mux 单独挂载，否则会被 SPA 兜底吞掉。
	router.MountThemeAssets(mux)
	mux.HandleFunc("/ws", removedMetricsRoute)
	mux.HandleFunc("/metrics/ws", removedMetricsRoute)
	registerPublicManifest(mux, config.staticDir, config.stores.siteSettings)
	mux.Handle("/", newSPAHandler(config.staticDir))
	operations.register(mux)
	alerts := alerter.New(alerter.Config{Rules: config.stores.alertRules, Events: config.stores.alertEvents, Metrics: config.stores.mts, Nodes: config.stores.nodes, Notifications: config.services.alertNotifications})
	// JSON 请求体预算在最外层生效：先有界，再认证，再解码。
	handler := middleware.JSONBodyLimit(int64(handler.MaxJSONBodyBytes))(mux)
	handler = middleware.Admission(limiter, ingressIdentity)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)
	handler = middleware.LoggingWithObserver(handler, metrics)
	handler = middleware.RequestContext(config.trustedProxies)(handler)
	return handler, alerts, fanout, nil
}

// runtimeDiagnosticsService 让 nil 服务保持可选，测试装配不需要诊断目录。
func runtimeDiagnosticsService(services *serverServices) *runtimediag.Service {
	if services == nil {
		return nil
	}
	return services.runtimeDiagnostics
}

func removedMetricsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}

func deploymentAccessRouterOption(access deploymentaccess.Config) api.RouterOption {
	return api.WithDeploymentAccess(access)
}
