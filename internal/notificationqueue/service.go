package notificationqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

type Service struct {
	db                *sql.DB
	secrets           secretbox.ResourceService
	sender            Sender
	now               func() time.Time
	random            io.Reader
	workers           int
	egress            *notification.Egress
	wake              chan struct{}
	mu                sync.Mutex
	active            map[string]activeDelivery
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	running           bool
	broken            atomic.Bool
	persistenceBroken atomic.Bool
	success, failed   atomic.Uint64
	observer          func(error)
	metricsMu         sync.Mutex
	dispatchMetrics   [len(metricProviders)]ProviderDispatchMetrics
}

func NewService(config Config) (*Service, error) {
	if config.Database == nil || config.Secrets == nil {
		return nil, ErrQueueUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Workers == 0 {
		// 固定上限的等待池，由 SQLite 中的 maximum_workers 限制实际派发并发。
		config.Workers = 32
	}
	if config.Workers < 1 || config.Workers > 32 {
		return nil, ErrInvalidRequest
	}
	if config.Egress == nil {
		config.Egress = notification.NewEgress()
	}
	service := &Service{db: config.Database, secrets: config.Secrets, sender: config.Sender, now: config.Now,
		random: &safeRandom{source: config.Random}, workers: config.Workers, egress: config.Egress, wake: make(chan struct{}, 1), active: map[string]activeDelivery{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Ready(ctx); err != nil {
		return nil, err
	}
	return service, nil
}

func (service *Service) Ready(ctx context.Context) error {
	var phase string
	if err := service.db.QueryRowContext(ctx, "SELECT phase FROM notification_secret_state WHERE id=1").Scan(&phase); err != nil {
		return ErrQueueUnavailable
	}
	if phase != "ready" || service.broken.Load() || service.persistenceBroken.Load() {
		return ErrQueueUnavailable
	}
	var invalid int
	err := service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_channels c
		LEFT JOIN alert_channel_revisions r ON r.channel_id=c.id AND r.revision=c.current_revision
		LEFT JOIN notification_channel_health h ON h.channel_id=c.id
		WHERE c.current_revision<1 OR r.id IS NULL OR h.channel_id IS NULL OR h.current_revision<>c.current_revision`).Scan(&invalid)
	if err != nil || invalid != 0 {
		return ErrQueueUnavailable
	}
	return nil
}

func (service *Service) newID() (string, error) {
	id, err := uuid.NewRandomFromReader(service.random)
	if err != nil {
		return "", ErrQueueUnavailable
	}
	return id.String(), nil
}

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *Service) SetObserver(observer func(error)) {
	service.mu.Lock()
	service.observer = observer
	service.mu.Unlock()
}

func (service *Service) observe(err error) {
	service.mu.Lock()
	observer := service.observer
	service.mu.Unlock()
	if observer != nil {
		observer(err)
	}
}

// reportFailure 上报一次领取或执行失败。关闭期间上下文取消会让 SQLite 自动回滚，驱动随后把
// 后续语句报成 sql.ErrTxDone 一类存储错误；这类错误不是健康故障，必须归一成取消后上报，
// 否则监督器会看到「观察者报失败、循环报成功」的不一致。
func (service *Service) reportFailure(ctx context.Context, err error) {
	if ctx.Err() != nil {
		service.observe(ctx.Err())
		return
	}
	service.observe(err)
}

func (service *Service) Stats() notification.DeliveryStats {
	return notification.DeliveryStats{Success: service.success.Load(), Failed: service.failed.Load()}
}

func (service *Service) Wait() { service.wg.Wait() }

func (service *Service) Close() {
	service.mu.Lock()
	if service.cancel != nil {
		service.cancel()
	}
	for _, active := range service.active {
		active.cancel()
	}
	service.mu.Unlock()
	service.Wait()
}

type safeRandom struct {
	mu     sync.Mutex
	source io.Reader
}

func (random *safeRandom) Read(value []byte) (int, error) {
	random.mu.Lock()
	defer random.mu.Unlock()
	return random.source.Read(value)
}

func exactlyOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("notification write affected an unexpected number of rows")
	}
	return nil
}
