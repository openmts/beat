package adminauth

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/model"
)

// SecurityMetricsSnapshot 是登录安全计数快照，标签只使用固定词表。
type SecurityMetricsSnapshot struct {
	Kinds               map[string]uint64
	Severities          map[string]uint64
	Reasons             map[string]uint64
	TransactionFailures uint64
	OutboxFailures      uint64
}

type securityCounters struct {
	mu                  sync.Mutex
	kinds               map[string]uint64
	severities          map[string]uint64
	reasons             map[string]uint64
	transactionFailures atomic.Uint64
	outboxFailures      atomic.Uint64
}

func newSecurityCounters() *securityCounters {
	return &securityCounters{
		kinds:      make(map[string]uint64, 2),
		severities: make(map[string]uint64, 3),
		reasons:    make(map[string]uint64, len(model.SecurityReasonCodes())),
	}
}

func (counters *securityCounters) observe(events []model.SecurityEvent) {
	counters.mu.Lock()
	defer counters.mu.Unlock()
	for _, event := range events {
		counters.kinds[event.Kind]++
		counters.severities[event.Severity]++
		for _, reason := range event.Reasons {
			counters.reasons[reason]++
		}
	}
}

func (counters *securityCounters) snapshot() SecurityMetricsSnapshot {
	counters.mu.Lock()
	defer counters.mu.Unlock()
	return SecurityMetricsSnapshot{
		Kinds:               copyCounters(counters.kinds),
		Severities:          copyCounters(counters.severities),
		Reasons:             copyCounters(counters.reasons),
		TransactionFailures: counters.transactionFailures.Load(),
		OutboxFailures:      counters.outboxFailures.Load(),
	}
}

func copyCounters(source map[string]uint64) map[string]uint64 {
	copied := make(map[string]uint64, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

// SecurityMetricsSnapshot 返回登录安全计数，供 Prometheus 文本端点使用。
func (service *Service) SecurityMetricsSnapshot() SecurityMetricsSnapshot {
	if service.security == nil {
		return SecurityMetricsSnapshot{}
	}
	return service.security.snapshot()
}

func (service *Service) observeSecurityEvents(events []model.SecurityEvent) {
	if service.security != nil {
		service.security.observe(events)
	}
}

func (service *Service) observeSecurityTransactionFailure() {
	if service.security != nil {
		service.security.transactionFailures.Add(1)
	}
}

func (service *Service) observeSecurityOutboxFailure() {
	if service.security != nil {
		service.security.outboxFailures.Add(1)
	}
}

// SecurityOutboxAge 返回最旧的未投递安全通知年龄。
func (service *Service) SecurityOutboxAge(ctx context.Context, now time.Time) (float64, bool, error) {
	return service.config.Store.SecurityOutboxAge(ctx, now)
}
