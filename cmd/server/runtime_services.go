package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/applicationgc"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/metricserasure"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/sshclient"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
	"github.com/beat/backend/internal/trafficreport"
)

func newServerServices(
	ctx context.Context, dataDir string, stores *serverStores,
) (*serverServices, error) {
	if err := openServerSecrets(ctx, dataDir, stores); err != nil {
		return nil, err
	}
	delivery, err := notificationqueue.NewService(notificationqueue.Config{Database: stores.sqlite.DB, Secrets: stores.secrets, Sender: notification.NewDispatcher()})
	if err != nil {
		return nil, fmt.Errorf("initialize notification queue: %w", err)
	}
	security, maintenanceService, backupService, err := newAdministrationServices(ctx, dataDir, stores, delivery)
	if err != nil {
		return nil, err
	}
	sshConnector, err := sshclient.New(filepath.Join(dataDir, "ssh", "known_hosts"))
	if err != nil {
		return nil, fmt.Errorf("initialize ssh client: %w", err)
	}
	oidcService := newOIDCService(ctx, stores, security, dataDir)
	themeService, themeMarkets, err := newThemeServices(ctx, stores, dataDir)
	if err != nil {
		return nil, err
	}
	recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelRecovery()
	if err := stores.applicationGc.RecoverInterrupted(recoveryCtx); err != nil {
		return nil, fmt.Errorf("recover application history cleanup: %w", err)
	}
	services := &serverServices{security: security, maintenance: maintenanceService, backups: backupService, metricRetention: serverMetricRetention(stores),
		metricErasure: metricserasure.NewService(stores.metricErasure, stores.mts),
		terminal:      service.NewTerminalService(stores.nodes, stores.sshKeys, sshConnector), delivery: delivery,
		trafficReports: trafficreport.NewService(trafficreport.Config{Schedules: stores.trafficReports, Nodes: stores.nodes, Metrics: stores.mts, Queue: delivery}),
		fleetSummaries: fleetsummary.NewService(fleetsummary.Config{
			Schedules: stores.fleetSummaries, Nodes: stores.nodes, Groups: stores.groups, Queue: delivery}),
		applicationGc:      applicationgc.New(ctx, stores.applicationGc),
		alertNotifications: service.NewAlertNotificationService(stores.alertEvents, delivery),
		oidc:               oidcService, themes: themeService, themeMarkets: themeMarkets}
	if err := services.initializeCommercial(ctx, stores, dataDir); err != nil {
		services.wait()
		return nil, err
	}
	return services, nil
}

// newThemeServices 构造主题与市场服务。
func newThemeServices(
	ctx context.Context, stores *serverStores, dataDir string,
) (*theme.Service, *theme.MarketService, error) {
	themeService, err := theme.NewService(stores.themes, stores.admins,
		filepath.Join(dataDir, "themes"), buildinfo.Version, time.Now)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize theme service: %w", err)
	}
	allowPrivate, err := themeMarketAllowPrivate()
	if err != nil {
		return nil, nil, err
	}
	if allowPrivate {
		slog.WarnContext(ctx, "theme market private sources are allowed", "reason", "operator_opt_in")
	}
	marketClient := theme.NewMarketClient(allowPrivate, time.Now)
	marketService := theme.NewMarketService(stores.themes, marketClient, themeService, time.Now)
	return themeService, marketService, nil
}

// themeMarketAllowPrivate 读取主题市场私网放通开关。缺省关闭；只有显式 true 才允许
// 指向私网、回环或保留地址的市场来源，供离线机房自建市场使用。
func themeMarketAllowPrivate() (bool, error) {
	value := os.Getenv("BEAT_THEME_MARKET_ALLOW_PRIVATE")
	if value == "" {
		return false, nil
	}
	allowed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("BEAT_THEME_MARKET_ALLOW_PRIVATE must be a boolean")
	}
	return allowed, nil
}

// newOIDCService 构造带生产 SSRF 策略的 OIDC 服务。
func newOIDCService(ctx context.Context, stores *serverStores, security *adminauth.Service, dataDir string) *oidc.Service {
	policy := oidc.EndpointPolicy{}
	client := oidc.NewProviderClient(policy, 15*time.Minute, time.Now)
	return oidc.NewService(stores.oidc, stores.admins, stores.secrets, security, client, time.Now, nil)
}

// openServerSecrets 先加载应用密钥，使通知队列与认证服务共享同一密钥服务。
func openServerSecrets(ctx context.Context, dataDir string, stores *serverStores) error {
	secrets, err := applicationsecrets.Open(ctx, applicationsecrets.Config{Database: stores.sqlite.DB, DataDir: dataDir})
	if err != nil {
		return fmt.Errorf("initialize administrator data encryption: %w", err)
	}
	stores.secrets = secrets
	stores.sshKeys.SetSecrets(secrets)
	return nil
}

func newAdministrationServices(
	ctx context.Context, dataDir string, stores *serverStores, queue notificationqueue.BatchEnqueuer,
) (*adminauth.Service, *maintenance.Service, *backup.Service, error) {
	keyPath := filepath.Join(dataDir, "admin-data.key")
	secrets := stores.secrets
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: stores.admins, Secrets: secrets, LegacyToken: os.Getenv("BEAT_ADMIN_TOKEN"), Queue: queue,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize administrator authentication: %w", err)
	}
	maintenanceService, err := maintenance.NewService(ctx, stores.maintenanceSetting,
		stores.sqlite, stores.mts, maintenance.WithAuditStore(stores.admins), maintenance.WithSecurityStore(stores.admins))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize maintenance service: %w", err)
	}
	backupService, err := backup.NewService(ctx, stores.backups, stores.sqlite, stores.mts,
		filepath.Join(dataDir, "backups"), keyPath, buildinfo.Version)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize backup service: %w", err)
	}
	backupService.SetThemesDir(filepath.Join(dataDir, "themes"))
	backupService.SetSecretLifecycle(secrets)
	backupService.SetUploadSessions(store.NewBackupUploadStore(stores.sqlite.DB))
	return security, maintenanceService, backupService, nil
}
