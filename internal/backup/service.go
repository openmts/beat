package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	MaximumUploadBytes   int64 = 4 << 30
	MaximumExpandedBytes int64 = 8 << 30
	RestoreConfirmation        = "RESTORE BEAT"
)

var (
	ErrAlreadyRunning = errors.New("backup is already running")
	ErrNotFound       = errors.New("backup not found")
	ErrInvalidConfirm = errors.New("restore confirmation phrase is invalid")
	ErrRestoreStaged  = errors.New("another restore is already staged")
)

type recordStore interface {
	Create(context.Context, *model.BackupRecord) error
	Update(context.Context, *model.BackupRecord) error
	Get(context.Context, string) (*model.BackupRecord, error)
	ListBackupsPage(context.Context, store.BackupQuery) (model.BackupPage, error)
	Delete(context.Context, string) error
	Transition(context.Context, store.BackupTransition) error
	Audit(context.Context, *model.AdminAuditEvent) error
	ExpireTerminal(context.Context, []string, time.Time) (int64, error)
	RecordQuarantine(context.Context, *model.BackupQuarantineRecord) error
	CountQuarantine(context.Context) (int, error)
	ListQuarantine(context.Context, int) ([]model.BackupQuarantineRecord, error)
}

type Service struct {
	ctx             context.Context
	records         recordStore
	sqlite          *store.SQLiteStore
	mts             *store.MTSStore
	root            string
	dataKeyPath     string
	version         string
	now             func() time.Time
	mu              sync.Mutex
	running         bool
	wg              sync.WaitGroup
	themesDir       string
	secretLifecycle SecretLifecycle
	geoipAssets     GeoIPAssets
	externalMMDB    string
	uploads         UploadSessionStore
	uploadMu        sync.Mutex
}

// SetThemesDir 配置主题包数据目录，用于备份格式 v2 的主题资产归档。
func (service *Service) SetThemesDir(themesDir string) {
	service.themesDir = themesDir
}

func NewService(
	ctx context.Context,
	records recordStore,
	sqlite *store.SQLiteStore,
	mts *store.MTSStore,
	root string,
	dataKeyPath string,
	version string,
) (*Service, error) {
	if records == nil || sqlite == nil || mts == nil {
		return nil, errors.New("backup dependencies are required")
	}
	if err := mts.AttachMetricCatalog(ctx, store.NewMetricCatalogStore(sqlite.DB)); err != nil {
		return nil, fmt.Errorf("initialize backup metric inventory: %w", err)
	}
	if err := secureDirectory(root); err != nil {
		return nil, err
	}
	return &Service{ctx: ctx, records: records, sqlite: sqlite, mts: mts, root: root,
		dataKeyPath: dataKeyPath, version: version, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) Start(ctx context.Context) (model.BackupRecord, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.running {
		return model.BackupRecord{}, ErrAlreadyRunning
	}
	release, err := service.acquireSecretLifecycle(ctx)
	if err != nil {
		return model.BackupRecord{}, err
	}
	now := service.now()
	id := uuid.New().String()
	record := model.BackupRecord{ID: id, Filename: backupFilename(now, id), Source: "generated",
		State: model.BackupStateRunning, CreatedAt: now}
	if err := service.records.Create(ctx, &record); err != nil {
		release()
		return model.BackupRecord{}, err
	}
	service.running = true
	service.wg.Add(1)
	go service.execute(record, release)
	return record, nil
}

func (service *Service) execute(record model.BackupRecord, release func()) {
	defer service.wg.Done()
	defer release()
	result, err := service.createArchive(service.ctx, record)
	completed := service.now()
	result.CompletedAt = &completed
	result.UpdatedAt = completed
	if err != nil {
		result.State = model.BackupStateFailed
		result.ErrorMessage = err.Error()
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if updateErr := service.records.Update(persistCtx, &result); updateErr != nil {
		slog.ErrorContext(persistCtx, "persist backup result failed", "backup_id", result.ID, "error", updateErr)
	}
	service.mu.Lock()
	service.running = false
	service.mu.Unlock()
}

// ListQuarantine 只暴露隔离清单的只读视图，归档本身既不导入也不删除。
func (service *Service) ListQuarantine(ctx context.Context) ([]model.BackupQuarantineRecord, error) {
	return service.records.ListQuarantine(ctx, store.BackupMaximumPage)
}

// ListQuarantinedUploads 只暴露被拒上传会话的只读视图；没有分块上传实现时返回空清单。
func (service *Service) ListQuarantinedUploads(ctx context.Context) ([]model.BackupUploadSession, error) {
	if service.uploads == nil {
		return []model.BackupUploadSession{}, nil
	}
	return service.uploads.ListQuarantined(ctx, store.BackupMaximumPage)
}

// List 只返回有界的一页备份历史，由服务层补齐新的游标语义。
func (service *Service) List(ctx context.Context, query store.BackupQuery) (model.BackupPage, error) {
	return service.records.ListBackupsPage(ctx, query)
}

func (service *Service) Open(ctx context.Context, id string) (*os.File, model.BackupRecord, error) {
	record, err := service.record(ctx, id)
	if err != nil {
		return nil, model.BackupRecord{}, err
	}
	if record.State != model.BackupStateReady && record.State != model.BackupStateValidated &&
		record.State != model.BackupStateStaged {
		return nil, model.BackupRecord{}, errors.New("backup archive is not ready")
	}
	path, err := service.archivePath(record.Filename)
	if err != nil {
		return nil, model.BackupRecord{}, err
	}
	present, err := regularArchivePresent(path)
	if err != nil {
		return nil, model.BackupRecord{}, err
	}
	if !present {
		return nil, model.BackupRecord{}, errors.New("backup archive is missing")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, model.BackupRecord{}, fmt.Errorf("open backup archive: %w", err)
	}
	return file, *record, nil
}

func (service *Service) Wait() {
	service.wg.Wait()
}

func (service *Service) Running() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.running
}

func (service *Service) record(ctx context.Context, id string) (*model.BackupRecord, error) {
	record, err := service.records.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	return record, nil
}

func (service *Service) archivePath(filename string) (string, error) {
	if filename == "" || filepath.Base(filename) != filename {
		return "", errors.New("backup filename is invalid")
	}
	return filepath.Join(service.root, filename), nil
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure backup directory: %w", err)
	}
	return nil
}

func removeRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect backup archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup archive is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove backup archive: %w", err)
	}
	return nil
}

func copyBounded(destination *os.File, source io.Reader, maximum int64) (int64, error) {
	written, err := io.Copy(destination, io.LimitReader(source, maximum+1))
	if err != nil {
		return written, fmt.Errorf("copy backup archive: %w", err)
	}
	if written > maximum {
		return written, fmt.Errorf("backup archive exceeds %d bytes", maximum)
	}
	return written, nil
}
