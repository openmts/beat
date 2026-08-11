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
)

type recordStore interface {
	Create(context.Context, *model.BackupRecord) error
	Update(context.Context, *model.BackupRecord) error
	Get(context.Context, string) (*model.BackupRecord, error)
	List(context.Context) ([]model.BackupRecord, error)
	Delete(context.Context, string) error
}

type Service struct {
	ctx         context.Context
	records     recordStore
	sqlite      *store.SQLiteStore
	mts         *store.MTSStore
	root        string
	dataKeyPath string
	version     string
	now         func() time.Time
	mu          sync.Mutex
	running     bool
	wg          sync.WaitGroup
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
	now := service.now()
	id := uuid.New().String()
	record := model.BackupRecord{ID: id, Filename: backupFilename(now, id), Source: "generated",
		State: model.BackupStateRunning, CreatedAt: now}
	if err := service.records.Create(ctx, &record); err != nil {
		return model.BackupRecord{}, err
	}
	service.running = true
	service.wg.Add(1)
	go service.execute(record)
	return record, nil
}

func (service *Service) execute(record model.BackupRecord) {
	defer service.wg.Done()
	result, err := service.createArchive(service.ctx, record)
	completed := service.now()
	result.CompletedAt = &completed
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

func (service *Service) List(ctx context.Context) ([]model.BackupRecord, error) {
	return service.records.List(ctx)
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
	file, err := os.Open(path)
	if err != nil {
		return nil, model.BackupRecord{}, fmt.Errorf("open backup archive: %w", err)
	}
	return file, *record, nil
}

func (service *Service) Delete(ctx context.Context, id string) error {
	record, err := service.record(ctx, id)
	if err != nil {
		return err
	}
	if record.State == model.BackupStateStaged {
		return errors.New("staged backup cannot be deleted")
	}
	path, err := service.archivePath(record.Filename)
	if err != nil {
		return err
	}
	if err := removeRegularFile(path); err != nil {
		return err
	}
	return service.records.Delete(ctx, id)
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
