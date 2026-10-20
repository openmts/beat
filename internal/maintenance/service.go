package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrAlreadyRunning = errors.New("maintenance is already running")

// securityRetentionBatch 限制单次维护删除的登录安全行数。
const securityRetentionBatch = 5000

type settingsStore interface {
	Get(context.Context) (model.MaintenanceSettings, model.MaintenanceRunStatus, error)
	Update(context.Context, model.MaintenanceSettings) (model.MaintenanceSettings, error)
	MarkStarted(context.Context, string, time.Time, time.Time) error
	MarkCompleted(context.Context, time.Time, time.Duration, string, string, string) error
	RecoverInterrupted(context.Context) error
}

type mtsOperations interface {
	CleanupBefore(context.Context, time.Time) error
	DiskUsage() (int64, error)
	Health() (bool, []string)
	// RepairCompactionIfDegraded 在分片压缩退化时就地整理一次，见 store 侧说明。
	RepairCompactionIfDegraded(context.Context) (bool, error)
}

type sqliteOperations interface {
	Maintain(context.Context) (string, error)
	DiskUsage() (int64, error)
}

type auditOperations interface {
	CleanupAuditEventsBefore(context.Context, time.Time) (int64, error)
}

// securityOperations 清理过期的登录安全事件、指纹与阈值窗口。
type securityOperations interface {
	PurgeSecurityData(context.Context, time.Time, int) (int64, error)
}

type ServiceOption func(*Service)

func WithAuditStore(store auditOperations) ServiceOption {
	return func(service *Service) {
		service.audit = store
	}
}

// WithSecurityStore 注册登录安全数据保留清理。
func WithSecurityStore(store securityOperations) ServiceOption {
	return func(service *Service) {
		service.security = store
	}
}

type Service struct {
	ctx      context.Context
	settings settingsStore
	mts      mtsOperations
	sqlite   sqliteOperations
	audit    auditOperations
	security securityOperations
	now      func() time.Time
	mu       sync.Mutex
	running  bool
	wg       sync.WaitGroup
}

func NewService(
	ctx context.Context,
	settings settingsStore,
	sqlite sqliteOperations,
	mts mtsOperations,
	options ...ServiceOption,
) (*Service, error) {
	service := &Service{
		ctx: ctx, settings: settings, sqlite: sqlite, mts: mts,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(service)
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := settings.RecoverInterrupted(recoveryCtx); err != nil {
		return nil, err
	}
	return service, nil
}

func (service *Service) Overview(ctx context.Context) (model.MaintenanceOverview, error) {
	settings, status, err := service.settings.Get(ctx)
	if err != nil {
		return model.MaintenanceOverview{}, err
	}
	mtsBytes, err := service.mts.DiskUsage()
	if err != nil {
		return model.MaintenanceOverview{}, err
	}
	sqliteBytes, err := service.sqlite.DiskUsage()
	if err != nil {
		return model.MaintenanceOverview{}, err
	}
	healthy, reasons := service.mts.Health()
	return model.MaintenanceOverview{
		Settings: settings, Status: status,
		Storage: model.StorageUsage{
			MTSBytes: mtsBytes, SQLiteBytes: sqliteBytes, TotalBytes: mtsBytes + sqliteBytes,
			MTSHealthy: healthy, MTSHealthReasons: reasons,
		},
	}, nil
}

func (service *Service) UpdateSettings(
	ctx context.Context,
	settings model.MaintenanceSettings,
) (model.MaintenanceSettings, error) {
	return service.settings.Update(ctx, settings)
}

func (service *Service) StartManual() error {
	return service.start(model.MaintenanceTriggerManual)
}

func (service *Service) StartAutomatic() error {
	return service.start(model.MaintenanceTriggerAutomatic)
}

func (service *Service) start(trigger string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.running {
		return ErrAlreadyRunning
	}
	settings, _, err := service.settings.Get(service.ctx)
	if err != nil {
		return fmt.Errorf("load maintenance settings: %w", err)
	}
	startedAt := service.now()
	cutoff := startedAt.AddDate(0, 0, -settings.RetentionDays)
	if err := service.settings.MarkStarted(service.ctx, trigger, startedAt, cutoff); err != nil {
		return err
	}
	service.running = true
	service.wg.Add(1)
	go service.execute(startedAt, cutoff)
	return nil
}

// RepairCompactionIfDegraded 在分片压缩退化时就地整理一次，与每日维护共用
// 「同一时刻只跑一个」的互斥；它不写维护状态行，也不删除保留窗口内的数据。
func (service *Service) RepairCompactionIfDegraded(ctx context.Context) (bool, error) {
	service.mu.Lock()
	if service.running {
		service.mu.Unlock()
		return false, ErrAlreadyRunning
	}
	service.running = true
	service.mu.Unlock()
	defer service.releaseRunning()
	return service.mts.RepairCompactionIfDegraded(ctx)
}

func (service *Service) releaseRunning() {
	service.mu.Lock()
	service.running = false
	service.mu.Unlock()
}

func (service *Service) execute(startedAt, cutoff time.Time) {
	defer service.wg.Done()
	integrity, runErr := service.run(service.ctx, cutoff)
	status, message := model.MaintenanceStatusSuccess, ""
	if runErr != nil {
		status, message = model.MaintenanceStatusFailed, runErr.Error()
	}
	completedAt := service.now()
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.settings.MarkCompleted(
		persistCtx, completedAt, completedAt.Sub(startedAt), status, message, integrity,
	); err != nil {
		slog.ErrorContext(persistCtx, "persist maintenance completion failed", "error", err)
	}
	service.mu.Lock()
	service.running = false
	service.mu.Unlock()
}

func (service *Service) run(ctx context.Context, cutoff time.Time) (string, error) {
	mtsErr := service.mts.CleanupBefore(ctx, cutoff)
	integrity, sqliteErr := service.sqlite.Maintain(ctx)
	var auditErr error
	if service.audit != nil {
		_, auditErr = service.audit.CleanupAuditEventsBefore(ctx, service.now().AddDate(0, 0, -180))
	}
	var securityErr error
	if service.security != nil {
		_, securityErr = service.security.PurgeSecurityData(ctx, service.now(), securityRetentionBatch)
	}
	if err := errors.Join(mtsErr, sqliteErr, auditErr, securityErr); err != nil {
		return integrity, fmt.Errorf("run maintenance: %w", err)
	}
	return integrity, nil
}

func (service *Service) Wait() {
	service.wg.Wait()
}

func (service *Service) Running() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.running
}
