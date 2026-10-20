package adminauth

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"
)

const (
	passwordConcurrentLimit   = 4
	passwordRequestsPerMinute = 120
	passwordRequestsPerSource = 20
	passwordBudgetSources     = 4096
	passwordBudgetWindow      = time.Minute
)

type authenticationBudgetEntry struct {
	start    time.Time
	requests int
}

type authenticationBudget struct {
	mu               sync.Mutex
	entries          map[[sha256.Size]byte]authenticationBudgetEntry
	start            time.Time
	requests, active int
}

func newAuthenticationBudget() *authenticationBudget {
	return &authenticationBudget{entries: make(map[[sha256.Size]byte]authenticationBudgetEntry)}
}

// 准入在账号查询与 Argon2 前计数，失败计数尚未返回时也不能穿透。
func (budget *authenticationBudget) acquire(source string, now time.Time) (func(), bool) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.prune(now)
	key := sha256.Sum256([]byte(source))
	entry, exists := budget.entries[key]
	if budget.active >= passwordConcurrentLimit || budget.requests >= passwordRequestsPerMinute ||
		entry.requests >= passwordRequestsPerSource || !exists && len(budget.entries) >= passwordBudgetSources {
		return nil, false
	}
	if !exists {
		entry.start = now
	}
	entry.requests++
	budget.entries[key] = entry
	budget.requests++
	budget.active++
	var once sync.Once
	return func() { once.Do(func() { budget.mu.Lock(); budget.active--; budget.mu.Unlock() }) }, true
}

func (budget *authenticationBudget) prune(now time.Time) {
	if budget.start.IsZero() || now.Sub(budget.start) >= passwordBudgetWindow {
		budget.start, budget.requests = now, 0
	}
	for key, entry := range budget.entries {
		if now.Sub(entry.start) >= passwordBudgetWindow {
			delete(budget.entries, key)
		}
	}
}

func (service *Service) admitPassword(ctx context.Context, source string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, allowed := service.passwordBudget.acquire(source, service.now())
	if !allowed {
		return nil, ErrLocked
	}
	return release, nil
}
