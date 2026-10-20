package applicationsecrets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

const (
	maximumDataKeys = 5
	batchSize       = 64
	keyRetention    = 7 * 24 * time.Hour
)

var (
	ErrUnhealthy  = errors.New("application secrets are unavailable")
	ErrCiphertext = errors.New("application secret failed authentication")
	ErrKeyLimit   = errors.New("application secret key retention limit reached")
)

type Config struct {
	Database *sql.DB
	DataDir  string
	Random   io.Reader
	Now      func() time.Time
	fault    func(string) error
}

type Service struct {
	db        *sql.DB
	directory string
	root      *secretbox.Manager
	ring      *keyring
	state     lifecycleState
	gate      sync.RWMutex
	lifecycle sync.RWMutex
	unhealthy atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	workerMu  sync.Mutex
	random    io.Reader
	now       func() time.Time
	fault     func(string) error
}

// Open 只在持有本地 Lease 后调用；迁移与恢复全部完成后才返回可用服务。
func Open(ctx context.Context, config Config) (*Service, error) {
	if config.Database == nil || config.DataDir == "" {
		return nil, errors.New("application secret dependencies are required")
	}
	if _, err := privateDirectoryOwner(config.DataDir); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	serviceCtx, cancel := context.WithCancel(ctx)
	service := &Service{db: config.Database, directory: config.DataDir, ctx: serviceCtx, cancel: cancel,
		random: &lockedReader{reader: config.Random}, now: config.Now, fault: config.fault}
	if err := service.initialize(ctx); err != nil {
		cancel()
		return nil, err
	}
	return service, nil
}

func (service *Service) Close() {
	service.workerMu.Lock()
	service.unhealthy.Store(true)
	service.cancel()
	service.workerMu.Unlock()
	service.wg.Wait()
}

func (service *Service) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service.gate.RLock()
	defer service.gate.RUnlock()
	if service.unhealthy.Load() || service.ring == nil || (service.state.Phase != "ready" && service.state.Phase != "rotating") {
		return ErrUnhealthy
	}
	return nil
}

func (service *Service) WithWrite(ctx context.Context, write func(secretbox.ResourceAccess) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service.gate.Lock()
	defer service.gate.Unlock()
	if service.unhealthy.Load() || service.ring == nil {
		return ErrUnhealthy
	}
	return write(service.ring)
}

func (service *Service) OpenResource(ctx context.Context, ciphertext []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service.gate.RLock()
	defer service.gate.RUnlock()
	if service.unhealthy.Load() || service.ring == nil {
		return nil, ErrUnhealthy
	}
	plaintext, err := service.ring.Open(ciphertext, aad)
	if err != nil {
		service.unhealthy.Store(true)
		return nil, ErrCiphertext
	}
	return plaintext, nil
}

func (service *Service) AcquireBackup(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !service.lifecycle.TryRLock() {
		return nil, ErrLifecycleBusy
	}
	if err := service.Ready(ctx); err != nil {
		service.lifecycle.RUnlock()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(service.lifecycle.RUnlock) }, nil
}

func (service *Service) checkpoint(phase string) error {
	if service.fault != nil {
		return service.fault(phase)
	}
	return nil
}

type lockedReader struct {
	mu     sync.Mutex
	reader io.Reader
}

func (reader *lockedReader) Read(value []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.reader.Read(value)
}
