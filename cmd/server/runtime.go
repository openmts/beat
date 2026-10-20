package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/beat/backend/internal/accelerators"
	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/alerter"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/applicationgc"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/diagnostics"
	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricserasure"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/nodelifecycle"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/remoteoperations"
	"github.com/beat/backend/internal/retentionadmin"
	"github.com/beat/backend/internal/runtimediag"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/serviceprincipalhttp"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
	"github.com/beat/backend/internal/trafficreport"
)

type serverStores struct {
	sqlite             *store.SQLiteStore
	mts                *store.MTSStore
	nodes              *store.NodeStore
	groups             *store.GroupStore
	sshKeys            *store.SSHKeyStore
	alertRules         *store.AlertRuleStore
	alertChannels      *store.AlertChannelStore
	alertEvents        *store.AlertEventStore
	trafficReports     *store.TrafficReportScheduleStore
	fleetSummaries     *store.FleetSummaryStore
	applicationGc      *store.ApplicationRetentionStore
	networkTasks       *store.NetworkTaskStore
	siteSettings       *store.SiteSettingsStore
	publicTelemetry    *store.PublicTelemetryStore
	trafficCalibration *store.TrafficCalibrationStore
	metricErasure      *store.MetricErasureStore
	maintenanceSetting *store.MaintenanceSettingsStore
	admins             *store.AdminStore
	backups            *store.BackupStore
	agentTasks         *store.AgentTaskStore
	customMetrics      *store.CustomMetricStore
	oidc               *store.OIDCStore
	themes             *store.ThemeStore
	secrets            *applicationsecrets.Service
}

type serverServices struct {
	agentRollouts              *agentrollout.Service
	agentRetention             agentrollout.RetentionPolicy
	agentRetentionAdmin        *retentionadmin.Service
	servicePrincipals          *serviceprincipalhttp.Handler
	serviceTokenAuthenticator  *serviceprincipal.Authenticator
	serviceTokenLimiter        *serviceprincipal.Limiter
	serviceTokenFailureLimiter *serviceprincipal.Limiter
	servicePrincipalStore      *store.ServicePrincipalStore
	accelerators               *accelerators.Service
	geoIP                      *geoip.Service
	nodeLifecycle              *nodelifecycle.Service
	operations                 *remoteoperations.Service
	diagnostics                *diagnostics.Service
	operationSpool             *remoteoperations.Spool
	operationClose             sync.Once
	evaluation                 *service.AlertEvaluator
	evaluationRules            *store.AlertEvaluationRuleStore
	evaluationStates           *store.AlertEvaluationStore
	security                   *adminauth.Service
	maintenance                *maintenance.Service
	metricRetention            *metriccatalog.RetentionRunner
	metricErasure              *metricserasure.Service
	backups                    *backup.Service
	terminal                   *service.TerminalService
	delivery                   *notificationqueue.Service
	alertNotifications         *service.AlertNotificationService
	trafficReports             *trafficreport.Service
	fleetSummaries             *fleetsummary.Service
	applicationGc              *applicationgc.Service
	oidc                       *oidc.Service
	themes                     *theme.Service
	themeMarkets               *theme.MarketService
	runtimeDiagnostics         *runtimediag.Service
	reconciliation             backup.ReconcileOutcome
}

type backgroundTasks struct {
	cancel  context.CancelFunc
	alerts  *alerter.Alerter
	runtime *runtimeState
	fanout  *metricsfanout.Service
	wait    sync.WaitGroup
	once    sync.Once
}

type serverRunConfig struct{ dbPath, mtsPath, listenAddr, staticDir string }

func run(ctx context.Context, config serverRunConfig) error {
	dbPath, mtsPath := config.dbPath, config.mtsPath
	lease, err := acquireServerLease(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	dataDir, err := prepareDataDirectory(runCtx, dbPath, mtsPath)
	if err != nil {
		return err
	}
	trustedProxies, err := middleware.ParseTrustedProxies(os.Getenv("BEAT_TRUSTED_PROXIES"))
	if err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}
	// 部署访问策略在启动时解析一次；非法配置必须让进程启动失败，而不是降级放行。
	deploymentAccess, err := deploymentaccess.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("configure deployment access: %w", err)
	}
	stores, err := openServerStores(runCtx, dbPath, mtsPath)
	if err != nil {
		return err
	}
	defer stores.closeIgnoringErrors()
	services, err := newServerServices(runCtx, dataDir, stores)
	if err != nil {
		stores.closeIgnoringErrors()
		return err
	}
	// 启动对账必须在 HTTP 之前完成：它把记录、归档与恢复 journal 对齐后再对外服务。
	reconciliation, err := services.backups.Reconcile(runCtx)
	if err != nil {
		return fmt.Errorf("reconcile backups: %w", err)
	}
	services.reconciliation = reconciliation
	defer func() {
		cancelRun()
		services.wait()
	}()
	return serveRuntime(runCtx, cancelRun, runtimeServingConfig{dataDir: dataDir, staticDir: config.staticDir, listenAddr: config.listenAddr,
		stores: stores, services: services, trustedProxies: trustedProxies, deploymentAccess: deploymentAccess})
}

type runtimeServingConfig struct {
	dataDir, staticDir, listenAddr string
	stores                         *serverStores
	services                       *serverServices
	trustedProxies                 middleware.TrustedProxies
	deploymentAccess               deploymentaccess.Config
}

func serveRuntime(runCtx context.Context, cancelRun context.CancelFunc, config runtimeServingConfig) error {
	runtime := newRuntimeState()
	handler, alerts, fanout, err := buildHTTPHandler(httpHandlerConfig{
		dataDir: config.dataDir, staticDir: config.staticDir, stores: config.stores, services: config.services,
		trustedProxies: config.trustedProxies, runtime: runtime, deploymentAccess: deploymentAccessOrDefault(config.deploymentAccess),
	})
	if err != nil {
		return err
	}
	workerCtx, cancelWorkers := context.WithCancel(context.WithoutCancel(runCtx))
	defer cancelWorkers()
	runtime.workers.bind(workerCtx, cancelRun)
	background := startBackgroundTasks(backgroundConfig{
		ctx: workerCtx, cancel: cancelWorkers, alerts: alerts, services: config.services, stores: config.stores,
		runtime: runtime, fanout: fanout,
	})
	defer background.stop()
	if err := serveHTTP(runCtx, httpServerConfig{address: config.listenAddr, handler: handler, beforeShutdown: func() {
		runtime.beginShutdown()
		_ = fanout.Close()
		config.services.terminal.CloseAll()
	}}); err != nil {
		return err
	}
	background.stop()
	cancelRun()
	config.services.wait()
	if runtime.workers.terminal.Load() {
		return fmt.Errorf("required background worker terminated")
	}
	return config.stores.close()
}

func prepareDataDirectory(ctx context.Context, dbPath, mtsPath string) (string, error) {
	dataDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("secure data directory: %w", err)
	}
	if err := applicationsecrets.RecoverRootFiles(ctx, dataDir, dbPath); err != nil {
		return "", fmt.Errorf("recover application root key: %w", err)
	}
	if err := backup.ApplyPending(ctx, dataDir, dbPath, mtsPath); err != nil {
		return "", fmt.Errorf("apply pending restore: %w", err)
	}
	return dataDir, nil
}

func openServerStores(ctx context.Context, dbPath, mtsPath string) (*serverStores, error) {
	marker := migrationstatus.NewWriter(filepath.Dir(dbPath), buildinfo.Version, buildinfo.Commit)
	if err := marker.Reset(); err != nil {
		log.Printf("migration status marker reset failed: %v", err)
	}
	sqliteStore, err := store.NewSQLiteStoreObserved(ctx, dbPath, migrationstatus.NewObserver(marker))
	if err != nil {
		return nil, fmt.Errorf("initialize sqlite store: %w", err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		_ = sqliteStore.Close()
		return nil, fmt.Errorf("secure sqlite database: %w", err)
	}
	mtsStore, err := store.NewMTSStore(mtsPath)
	if err != nil {
		_ = sqliteStore.Close()
		return nil, fmt.Errorf("initialize mts store: %w", err)
	}
	database := sqliteStore.DB
	nodeStore := store.NewNodeStore(database)
	mtsStore.WithTrafficCycleSource(func(ctx context.Context) ([]model.Node, error) {
		return nodeStore.ListNodes(ctx, "")
	})
	return &serverStores{sqlite: sqliteStore, mts: mtsStore,
		nodes: nodeStore, groups: store.NewGroupStore(database),
		sshKeys: store.NewSSHKeyStore(database), alertRules: store.NewAlertRuleStore(database),
		alertChannels: store.NewAlertChannelStore(database), alertEvents: store.NewAlertEventStore(database),
		trafficReports: store.NewTrafficReportScheduleStore(database),
		fleetSummaries: store.NewFleetSummaryStore(database),
		applicationGc:  store.NewApplicationRetentionStore(database),
		networkTasks:   store.NewNetworkTaskStore(database), siteSettings: store.NewSiteSettingsStore(database),
		publicTelemetry:    store.NewPublicTelemetryStore(database),
		trafficCalibration: store.NewTrafficCalibrationStore(database),
		metricErasure:      store.NewMetricErasureStore(database),
		maintenanceSetting: store.NewMaintenanceSettingsStore(database), admins: store.NewAdminStore(database),
		backups: store.NewBackupStore(database), agentTasks: store.NewAgentTaskStore(database),
		customMetrics: store.NewCustomMetricStore(database), oidc: store.NewOIDCStore(database),
		themes: store.NewThemeStore(database)}, nil
}

// deploymentAccessOrDefault 让未显式注入策略的调用方（含测试）保留历史行为。
func deploymentAccessOrDefault(access deploymentaccess.Config) deploymentaccess.Config {
	if access.APIOrigins().Mode() != deploymentaccess.OriginModeSameOrigin || access.PublicBaseURL() != "" {
		return access
	}
	fallback, err := deploymentaccess.Load(func(string) string { return "" })
	if err != nil {
		return access
	}
	return fallback
}
