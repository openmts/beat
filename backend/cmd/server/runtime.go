package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/alerter"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/sshclient"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/trafficreport"
	"github.com/beat/backend/internal/websocket"
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
	networkTasks       *store.NetworkTaskStore
	siteSettings       *store.SiteSettingsStore
	maintenanceSetting *store.MaintenanceSettingsStore
	admins             *store.AdminStore
	backups            *store.BackupStore
}

type serverServices struct {
	security       *adminauth.Service
	maintenance    *maintenance.Service
	backups        *backup.Service
	terminal       *service.TerminalService
	delivery       *notification.Service
	trafficReports *trafficreport.Service
}

type backgroundTasks struct {
	cancel  context.CancelFunc
	alerts  *alerter.Alerter
	runtime *runtimeState
	wait    sync.WaitGroup
	once    sync.Once
}

func run(ctx context.Context, dbPath, mtsPath, listenAddr, staticDir string) error {
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
	stores, err := openServerStores(runCtx, dbPath, mtsPath)
	if err != nil {
		return err
	}
	services, err := newServerServices(runCtx, dataDir, stores)
	if err != nil {
		stores.closeIgnoringErrors()
		return err
	}
	defer func() {
		cancelRun()
		services.wait()
	}()
	runtime := &runtimeState{}
	handler, alerts := buildHTTPHandler(httpHandlerConfig{
		dataDir: dataDir, staticDir: staticDir, stores: stores, services: services,
		trustedProxies: trustedProxies, runtime: runtime,
	})
	background := startBackgroundTasks(backgroundConfig{
		ctx: runCtx, cancel: cancelRun, alerts: alerts, services: services, runtime: runtime,
	})
	defer background.stop()
	if err := serveHTTP(ctx, listenAddr, handler, func() {
		background.stop()
		services.maintenance.Wait()
	}); err != nil {
		return err
	}
	return stores.close()
}

func prepareDataDirectory(ctx context.Context, dbPath, mtsPath string) (string, error) {
	dataDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("secure data directory: %w", err)
	}
	if err := backup.ApplyPending(ctx, dataDir, dbPath, mtsPath); err != nil {
		return "", fmt.Errorf("apply pending restore: %w", err)
	}
	return dataDir, nil
}

func openServerStores(ctx context.Context, dbPath, mtsPath string) (*serverStores, error) {
	sqliteStore, err := store.NewSQLiteStoreContext(ctx, dbPath)
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
	return &serverStores{sqlite: sqliteStore, mts: mtsStore,
		nodes: store.NewNodeStore(database), groups: store.NewGroupStore(database),
		sshKeys: store.NewSSHKeyStore(database), alertRules: store.NewAlertRuleStore(database),
		alertChannels: store.NewAlertChannelStore(database), alertEvents: store.NewAlertEventStore(database),
		trafficReports: store.NewTrafficReportScheduleStore(database),
		networkTasks:   store.NewNetworkTaskStore(database), siteSettings: store.NewSiteSettingsStore(database),
		maintenanceSetting: store.NewMaintenanceSettingsStore(database), admins: store.NewAdminStore(database),
		backups: store.NewBackupStore(database)}, nil
}

func newServerServices(
	ctx context.Context, dataDir string, stores *serverStores,
) (*serverServices, error) {
	security, maintenanceService, backupService, err := newAdministrationServices(ctx, dataDir, stores)
	if err != nil {
		return nil, err
	}
	sshConnector, err := sshclient.New(filepath.Join(dataDir, "ssh", "known_hosts"))
	if err != nil {
		return nil, fmt.Errorf("initialize ssh client: %w", err)
	}
	delivery := notification.NewService()
	return &serverServices{security: security, maintenance: maintenanceService, backups: backupService,
		terminal: service.NewTerminalService(stores.nodes, stores.sshKeys, sshConnector), delivery: delivery,
		trafficReports: trafficreport.NewService(stores.trafficReports, stores.nodes,
			stores.alertChannels, stores.mts, delivery)}, nil
}

func newAdministrationServices(
	ctx context.Context, dataDir string, stores *serverStores,
) (*adminauth.Service, *maintenance.Service, *backup.Service, error) {
	keyPath := filepath.Join(dataDir, "admin-data.key")
	secrets, err := secretbox.New(keyPath, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize administrator data encryption: %w", err)
	}
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: stores.admins, Secrets: secrets, LegacyToken: os.Getenv("BEAT_ADMIN_TOKEN"),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize administrator authentication: %w", err)
	}
	maintenanceService, err := maintenance.NewService(ctx, stores.maintenanceSetting,
		stores.sqlite, stores.mts, maintenance.WithAuditStore(stores.admins))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize maintenance service: %w", err)
	}
	backupService, err := backup.NewService(ctx, stores.backups, stores.sqlite, stores.mts,
		filepath.Join(dataDir, "backups"), keyPath, buildinfo.Version)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize backup service: %w", err)
	}
	return security, maintenanceService, backupService, nil
}

type httpHandlerConfig struct {
	dataDir        string
	staticDir      string
	stores         *serverStores
	services       *serverServices
	trustedProxies middleware.TrustedProxies
	runtime        *runtimeState
}

func buildHTTPHandler(config httpHandlerConfig) (http.Handler, *alerter.Alerter) {
	metrics := observability.NewRegistry()
	hub := websocket.NewMetricsHub()
	go hub.Run()
	router := api.NewRouter(config.stores.nodes, config.stores.groups, config.stores.sshKeys, config.stores.alertRules,
		config.stores.alertChannels, config.stores.alertEvents, config.stores.mts,
		api.WithAdminToken(os.Getenv("BEAT_ADMIN_TOKEN")),
		api.WithAgentAuthentication(os.Getenv("BEAT_AGENT_TOKEN")),
		api.WithTerminalOperations(config.services.terminal), api.WithNetworkTaskStore(config.stores.networkTasks),
		api.WithSiteSettingsStore(config.stores.siteSettings), api.WithNotificationService(config.services.delivery),
		api.WithTrafficReportService(config.services.trafficReports), api.WithMaintenanceService(config.services.maintenance),
		api.WithAdminSecurity(config.services.security), api.WithBackupService(config.services.backups))
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", router.ServeHandler())
	mux.Handle("/ws", websocket.NewHandler(hub))
	mux.Handle("/metrics/ws", websocket.NewMetricsHandler(hub))
	mux.Handle("/", newSPAHandler(config.staticDir))
	newOperability(config, metrics).register(mux)
	alerts := alerter.New(config.stores.alertRules, config.stores.alertEvents, config.stores.alertChannels,
		config.stores.mts, config.stores.nodes, config.services.delivery)
	handler := middleware.SecurityHeaders(mux)
	handler = middleware.Recovery(handler)
	handler = middleware.LoggingWithObserver(handler, metrics)
	handler = middleware.RequestContext(config.trustedProxies)(handler)
	return handler, alerts
}

type backgroundConfig struct {
	ctx      context.Context
	cancel   context.CancelFunc
	alerts   *alerter.Alerter
	services *serverServices
	runtime  *runtimeState
}

func startBackgroundTasks(config backgroundConfig) *backgroundTasks {
	tasks := &backgroundTasks{cancel: config.cancel, alerts: config.alerts, runtime: config.runtime}
	tasks.wait.Go(func() { config.alerts.Start(config.ctx) })
	reportScheduler := trafficreport.NewScheduler(config.services.trafficReports)
	tasks.wait.Go(func() { reportScheduler.Run(config.ctx) })
	maintenanceScheduler := maintenance.NewScheduler(config.services.maintenance)
	tasks.wait.Go(func() { maintenanceScheduler.Run(config.ctx) })
	config.runtime.schedulers.Store(true)
	return tasks
}

func (tasks *backgroundTasks) stop() {
	tasks.once.Do(func() {
		tasks.cancel()
		tasks.runtime.schedulers.Store(false)
		tasks.alerts.Stop()
		tasks.wait.Wait()
	})
}

func serveHTTP(ctx context.Context, address string, handler http.Handler, beforeShutdown func()) error {
	server := &http.Server{Addr: address, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	errorsCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "server listening", "address", address, "version", buildinfo.Version)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorsCh <- fmt.Errorf("http server: %w", err)
		}
	}()
	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutdown requested", "reason", ctx.Err())
	case err := <-errorsCh:
		return err
	}
	beforeShutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	return nil
}

func (services *serverServices) wait() {
	services.maintenance.Wait()
	services.backups.Wait()
}

func (stores *serverStores) close() error {
	if err := stores.sqlite.Close(); err != nil {
		return fmt.Errorf("close sqlite store: %w", err)
	}
	if err := stores.mts.Close(); err != nil {
		return fmt.Errorf("close mts store: %w", err)
	}
	return nil
}

func (stores *serverStores) closeIgnoringErrors() {
	_ = stores.mts.Close()
	_ = stores.sqlite.Close()
}
