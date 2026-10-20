package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/ingress"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/serviceprincipalhttp"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
)

type Router struct {
	agentRollouts      *agentrollout.Service
	servicePrincipals  *serviceprincipalhttp.Handler
	serviceTokenAccess *ServiceTokenAccess
	rolloutRequests    *rolloutAdmission
	metricsHandler     *handler.MetricsHandler
	runtimeStatus      http.Handler
	runtimeDiagnostics handler.RuntimeDiagnostics
	runtimeAudit       handler.RuntimeAudit
	migrationOverview  handler.MigrationOverviewSource
	accelerators       handler.AcceleratorService
	geoIP              handler.GeoIPService
	locationViews      handler.NodeLocationViews
	nodeLifecycle      handler.NodeLifecycleService
	lifecycleViews     handler.NodeLifecycleViews
	remoteOperations   handler.RemoteOperations
	diagnostics        handler.Diagnostics
	alertEvaluation    *handler.AlertEvaluationHandlerConfig
	mux                *http.ServeMux
	nodeStore          *store.NodeStore
	groupStore         *store.GroupStore
	sshKeyStore        *store.SSHKeyStore
	alertRuleStore     *store.AlertRuleStore
	alertChannelStore  *store.AlertChannelStore
	alertEventStore    *store.AlertEventStore
	mtsStore           *store.MTSStore
	networkTaskStore   *store.NetworkTaskStore
	siteSettingsStore  *store.SiteSettingsStore
	publicTelemetry    *store.PublicTelemetryStore
	trafficCalibration *store.TrafficCalibrationStore
	metricErasure      *store.MetricErasureStore
	metricErasureWake  func()
	agentTasks         *store.AgentTaskStore
	customMetrics      *store.CustomMetricStore
	metricCatalog      *store.MetricCatalogStore
	metricQueries      *metricquery.Service
	metricDispatcher   handler.CustomMetricDispatcher
	delivery           *notification.Service
	trafficReports     handler.TrafficReportOperations
	fleetSummaries     handler.FleetSummaryOperations
	maintenance        handler.MaintenanceOperations
	// agentRetention 暴露发布对象保留期的后台配置与手动清理；未注入时不注册路由。
	agentRetention     handler.AgentRetentionOperations
	applicationGc      handler.ApplicationRetentionOperations
	backups            handler.BackupOperations
	enrollment         *store.AgentEnrollmentStore
	enrollmentMetrics  handler.EnrollmentMetrics
	security           *adminauth.Service
	oidc               *oidc.Service
	themeService       *theme.Service
	themeMarkets       *theme.MarketService
	adminToken         string
	legacyAgentToken   string
	terminal           handler.TerminalOperations
	adminAuthEnabled   bool
	agentAuthEnabled   bool
	dashboards         *store.DashboardStore
	dashboardPolicy    handler.DashboardPolicyReader
	applicationSecrets handler.ApplicationSecretOperations
	alertNotifications handler.AlertNotifications
	notificationQueue  *notificationqueue.Service
	deploymentAccess   deploymentaccess.Config
	ingressLimiter     IngressLimiter
}

// IngressLimiter 是 Agent 上报的按节点准入接口；nil 表示不启用。
type IngressLimiter interface {
	Admit(class ingress.Class, identity string) (ingress.Lease, *ingress.Rejection)
}

type RouterOption func(*Router)

// WithDeploymentAccess 注入启动时解析好的部署访问策略。
// 未注入时保留历史行为：公共 REST 允许任意来源，公共 WebSocket 不新增限制。
func WithDeploymentAccess(config deploymentaccess.Config) RouterOption {
	return func(router *Router) { router.deploymentAccess = config }
}

func WithAuthTokens(adminToken, agentToken string) RouterOption {
	return func(router *Router) {
		router.adminToken = adminToken
		router.legacyAgentToken = agentToken
		router.adminAuthEnabled = true
		router.agentAuthEnabled = true
	}
}

func WithAdminToken(adminToken string) RouterOption {
	return func(router *Router) {
		router.adminToken = adminToken
		router.adminAuthEnabled = true
	}
}

func WithAgentAuthentication(legacyAgentToken string) RouterOption {
	return func(router *Router) {
		router.legacyAgentToken = legacyAgentToken
		router.agentAuthEnabled = true
	}
}

// WithIngressLimiter 注入入口准入控制；未注入时按历史行为放行。
func WithIngressLimiter(limiter IngressLimiter) RouterOption {
	return func(router *Router) {
		router.ingressLimiter = limiter
	}
}

// WithMigrationOverview 注入所有者可见的只读迁移与恢复视图。
func WithMigrationOverview(source handler.MigrationOverviewSource) RouterOption {
	return func(router *Router) {
		router.migrationOverview = source
	}
}

// WithRuntimeDiagnostics 注入所有者可见的运行时诊断服务与审计记录器。
func WithRuntimeDiagnostics(service handler.RuntimeDiagnostics, audit handler.RuntimeAudit) RouterOption {
	return func(router *Router) {
		router.runtimeDiagnostics = service
		router.runtimeAudit = audit
	}
}

func WithTerminalOperations(terminal handler.TerminalOperations) RouterOption {
	return func(router *Router) {
		router.terminal = terminal
	}
}

func WithAgentTaskStore(agentTasks *store.AgentTaskStore) RouterOption {
	return func(router *Router) {
		router.agentTasks = agentTasks
	}
}

func WithCustomMetricStore(customMetrics *store.CustomMetricStore) RouterOption {
	return func(router *Router) {
		router.customMetrics = customMetrics
	}
}

func WithNetworkTaskStore(networkTaskStore *store.NetworkTaskStore) RouterOption {
	return func(router *Router) {
		router.networkTaskStore = networkTaskStore
	}
}

func WithSiteSettingsStore(siteSettingsStore *store.SiteSettingsStore) RouterOption {
	return func(router *Router) {
		router.siteSettingsStore = siteSettingsStore
	}
}

// WithPublicTelemetryStore 注入匿名聚合遥测策略与公共交互端点所需的存储。
func WithPublicTelemetryStore(publicTelemetry *store.PublicTelemetryStore) RouterOption {
	return func(router *Router) {
		router.publicTelemetry = publicTelemetry
	}
}

// WithTrafficCalibrationStore 注入追加式流量校准账本。
func WithTrafficCalibrationStore(trafficCalibration *store.TrafficCalibrationStore) RouterOption {
	return func(router *Router) {
		router.trafficCalibration = trafficCalibration
	}
}

// WithMetricErasureStore 注入耐久擦除任务账本；wake 在创建任务后立即唤醒 worker。
func WithMetricErasureStore(metricErasure *store.MetricErasureStore, wake func()) RouterOption {
	return func(router *Router) {
		router.metricErasure = metricErasure
		router.metricErasureWake = wake
	}
}

func WithNotificationService(delivery *notification.Service) RouterOption {
	return func(router *Router) {
		router.delivery = delivery
	}
}

func WithTrafficReportService(service handler.TrafficReportOperations) RouterOption {
	return func(router *Router) {
		router.trafficReports = service
	}
}

func WithFleetSummaryService(service handler.FleetSummaryOperations) RouterOption {
	return func(router *Router) {
		router.fleetSummaries = service
	}
}

func WithMaintenanceService(service handler.MaintenanceOperations) RouterOption {
	return func(router *Router) {
		router.maintenance = service
	}
}

// WithAgentRetentionService 注入发布对象保留期的后台读写面。
func WithAgentRetentionService(service handler.AgentRetentionOperations) RouterOption {
	return func(router *Router) {
		router.agentRetention = service
	}
}

func WithApplicationRetentionService(service handler.ApplicationRetentionOperations) RouterOption {
	return func(router *Router) {
		router.applicationGc = service
	}
}

func WithBackupService(service handler.BackupOperations) RouterOption {
	return func(router *Router) {
		router.backups = service
	}
}

// WithAgentEnrollmentStore 注入自动发现式入网存储；nil 时跳过相关路由注册。
func WithAgentEnrollmentStore(enrollment *store.AgentEnrollmentStore) RouterOption {
	return func(router *Router) {
		router.enrollment = enrollment
	}
}

// WithAgentEnrollmentMetrics 注入入网协议与管理端写路径的聚合观测；nil 时关闭观测。
func WithAgentEnrollmentMetrics(metrics handler.EnrollmentMetrics) RouterOption {
	return func(router *Router) {
		router.enrollmentMetrics = metrics
	}
}

func WithAdminSecurity(service *adminauth.Service) RouterOption {
	return func(router *Router) {
		router.security = service
	}
}

// WithOIDC 注入 OIDC 服务；nil 时跳过 OIDC 路由注册。
func WithOIDC(service *oidc.Service) RouterOption {
	return func(router *Router) {
		router.oidc = service
	}
}

// WithThemes 注入主题服务与市场服务；nil 时跳过主题路由注册。
func WithThemes(service *theme.Service, markets *theme.MarketService) RouterOption {
	return func(router *Router) {
		router.themeService = service
		router.themeMarkets = markets
	}
}

func NewRouter(
	nodeStore *store.NodeStore,
	groupStore *store.GroupStore,
	sshKeyStore *store.SSHKeyStore,
	alertRuleStore *store.AlertRuleStore,
	alertChannelStore *store.AlertChannelStore,
	alertEventStore *store.AlertEventStore,
	mtsStore *store.MTSStore,
	options ...RouterOption,
) *Router {
	r := &Router{
		mux:               http.NewServeMux(),
		nodeStore:         nodeStore,
		groupStore:        groupStore,
		sshKeyStore:       sshKeyStore,
		alertRuleStore:    alertRuleStore,
		alertChannelStore: alertChannelStore,
		alertEventStore:   alertEventStore,
		mtsStore:          mtsStore,
	}
	for _, option := range options {
		option(r)
	}
	if r.delivery == nil {
		r.delivery = notification.NewService()
	}

	r.setupRoutes()

	return r
}

func (r *Router) setupRoutes() {
	api := http.NewServeMux()
	// 指标处理器在注册路由前构造：公共节点视图与指标流共享同一份快照。
	r.metricsHandler = handler.NewMetricsHandler(r.nodeStore, r.mtsStore, r.siteSettingsStore).
		WithMetricCatalog(r.metricCatalog).WithLocationViews(r.locationViews).
		WithLifecycleViews(r.lifecycleViews).WithAccelerators(r.accelerators)
	r.registerAuthAndSettingsRoutes(api)
	r.registerNodeAndGroupRoutes(api)
	r.registerMetricCatalogRoutes(api)
	r.registerAgentEnrollmentRoutes(api)
	r.registerAgentRolloutRoutes(api)
	r.registerServicePrincipalRoutes(api)
	r.registerNotificationRoutes(api)
	r.registerAdminSecurityRoutes(api)
	r.registerApplicationSecretRoutes(api)
	r.registerMetricDashboardRoutes(api)
	r.registerOperationsRoutes(api)
	r.registerRuntimeDiagnosticsRoutes(api)
	r.registerMigrationOverviewRoutes(api)
	r.registerCommercialRoutes(api)
	r.registerNodeLifecycleRoutes(api)
	r.registerAcceleratorRoutes(api)
	r.registerNetworkRoutes(api)

	r.mux.Handle("/api/v1/", middleware.SecurityHeaders(
		middleware.CORSWithPolicy(r.effectiveAPIOrigins())(middleware.ContentTypeJSON(api)),
	))
}

// ThemeAssetHandler 返回公开的主题资源处理器；没有主题服务时为 nil。
// 该路由不在 /api/v1 前缀下，必须由最外层 mux 挂载（见 MountThemeAssets）。
func (r *Router) ThemeAssetHandler() http.Handler {
	if r.themeService == nil {
		return nil
	}
	return http.HandlerFunc(handler.NewThemeHandler(r.themeService, r.themeMarkets).HandleAsset)
}

// MountThemeAssets 把 /themes/assets/{digest}/{asset...} 挂到最外层 mux。
// 主题目录与沙箱运行时的资源都走这个不可变前缀，路由器自身只服务 /api/v1。
func (r *Router) MountThemeAssets(mux *http.ServeMux) {
	assets := r.ThemeAssetHandler()
	if assets == nil || mux == nil {
		return
	}
	mux.Handle("GET /themes/assets/{digest}/{asset...}", assets)
}

func (r *Router) admin(next http.Handler) http.Handler {
	if r.security != nil {
		return r.sessionAdmin(next)
	}
	if !r.adminAuthEnabled {
		return next
	}
	return middleware.BearerAuth(r.adminToken)(next)
}

func (r *Router) agent(next http.Handler, resolver middleware.LegacyNodeNameResolver) http.Handler {
	if !r.agentAuthEnabled {
		return next
	}
	return middleware.AgentAuth(r.nodeStore, r.legacyAgentToken, resolver)(next)
}

func (r *Router) websocketAdmin(next http.Handler) http.Handler {
	if r.security != nil {
		return r.sessionWebSocketAdmin(next)
	}
	if !r.adminAuthEnabled {
		return next
	}
	return middleware.WebSocketBearerAuth(r.adminToken)(next)
}

func queryNodeName(parameter string) middleware.LegacyNodeNameResolver {
	return func(r *http.Request) (string, bool) {
		value := r.URL.Query().Get(parameter)
		return value, value != ""
	}
}

func bodyNodeName(field string) middleware.LegacyNodeNameResolver {
	return func(r *http.Request) (string, bool) {
		const maximumBody = 1 << 20
		content, err := io.ReadAll(io.LimitReader(r.Body, maximumBody+1))
		if err != nil || len(content) > maximumBody {
			return "", false
		}
		r.Body = io.NopCloser(bytes.NewReader(content))
		var values map[string]json.RawMessage
		if err := json.Unmarshal(content, &values); err != nil {
			return "", false
		}
		var name string
		if err := json.Unmarshal(values[field], &name); err != nil || name == "" {
			return "", false
		}
		return name, true
	}
}

func (r *Router) ServeHandler() http.Handler {
	return r.mux
}
