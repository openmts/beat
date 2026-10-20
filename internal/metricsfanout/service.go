package metricsfanout

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

type generation struct {
	number         uint64
	at, storedAt   time.Time
	initialAge     time.Duration
	payload, frame []byte
}

type limits struct {
	global, perIP, tracked                   int
	globalBurst, globalRate, ipBurst, ipRate float64
}

type Service struct {
	config           Config
	limits           limits
	mu               sync.Mutex
	clients          map[*client]struct{}
	ips              map[netip.Addr]*ipEntry
	global           bucket
	reserved         int
	latest           *generation
	stats            Stats
	running, closing bool
	ctx              context.Context
	cancel           context.CancelFunc
	wait             sync.WaitGroup
	closeOnce        sync.Once
	observerFailed   atomic.Bool
	ticks            <-chan time.Time
	clockNow         func() time.Time
	wallFloor        time.Time
	timing           timing
}

func New(config Config) (*Service, error) {
	if config.Build == nil || config.ClientIP == nil || config.AllowOrigin == nil {
		return nil, ErrConfiguration
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{config: config, limits: limits{GlobalConnections, PerIPConnections, MaximumTrackedIPs, GlobalHandshakeBurst, GlobalHandshakesPerSecond, PerIPHandshakeBurst, PerIPHandshakesPerSecond}, clients: map[*client]struct{}{}, ips: map[netip.Addr]*ipEntry{}, ctx: ctx, cancel: cancel}
	s.clockNow = time.Now
	s.wallFloor = config.Now().UTC()
	s.timing = timing{WriteTimeout, ReadTimeout, PingInterval}
	s.global = bucket{tokens: s.limits.globalBurst, at: s.clockNow()}
	return s, nil
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.cancel()
		clients := make([]*client, 0, len(s.clients))
		for c := range s.clients {
			clients = append(clients, c)
		}
		s.mu.Unlock()
		for _, c := range clients {
			c.requestClose(1001, nil, ReasonClosed)
		}
		s.wait.Wait()
	})
	return nil
}

func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.stats
	value.Running = s.running
	value.Closed = s.closing
	value.ObserverFailed = s.observerFailed.Load()
	value.Connections = 0
	for c := range s.clients {
		if c.admitted.Load() {
			value.Connections++
		}
	}
	value.Reserved, value.TrackedIPs = s.reserved, len(s.ips)
	value.Generation = s.snapshotInfo()
	return value
}

// Latest 返回独立副本，HTTP WebSocket发送直接共享内部不可变frame，不调用此复制入口。
func (s *Service) Latest() (SnapshotInfo, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.snapshotInfo()
	if !info.Available {
		return info, nil
	}
	return info, append([]byte(nil), s.latest.payload...)
}

func (s *Service) snapshotInfo() SnapshotInfo {
	if s.latest == nil {
		return SnapshotInfo{}
	}
	age := s.ageLocked(s.latest)
	return SnapshotInfo{Generation: s.latest.number, Timestamp: s.latest.at, Age: age, Stale: age > StaleWindow || s.stats.ConsecutiveFailures > 0, Available: age <= StaleWindow}
}

func (s *Service) emit(event Event) {
	if s.config.Observer == nil || s.observerFailed.Load() {
		return
	}
	defer func() {
		if recover() != nil {
			s.observerFailed.Store(true)
			s.cancel()
		}
	}()
	s.config.Observer(event)
}

type timing struct{ write, read, ping time.Duration }

func (s *Service) effectiveNowLocked() time.Time {
	now := s.config.Now().UTC()
	if now.After(s.wallFloor) {
		s.wallFloor = now
	}
	return s.wallFloor
}

func (s *Service) ageLocked(value *generation) time.Duration {
	age := s.effectiveNowLocked().Sub(value.at)
	monotonic := s.clockNow().Sub(value.storedAt) + value.initialAge
	if monotonic > age {
		age = monotonic
	}
	if age < 0 {
		return 0
	}
	return age
}

func (s *Service) age(value *generation) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ageLocked(value)
}
