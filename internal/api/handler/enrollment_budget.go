package handler

import (
	"sync"
	"time"
)

const enrollmentBudgetWindow = time.Minute

type enrollmentBudgetPolicy struct{ perIP, total, concurrent, keys int }

type enrollmentBudgetEntry struct {
	start time.Time
	count int
}

type enrollmentBudget struct {
	mu            sync.Mutex
	policy        enrollmentBudgetPolicy
	entries       map[string]enrollmentBudgetEntry
	start         time.Time
	total, active int
}

func newEnrollmentBudget(policy enrollmentBudgetPolicy) *enrollmentBudget {
	return &enrollmentBudget{policy: policy, entries: make(map[string]enrollmentBudgetEntry)}
}

func (budget *enrollmentBudget) acquire(key string, now time.Time) (func(), bool) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.prune(now)
	entry, exists := budget.entries[key]
	if budget.active >= budget.policy.concurrent || budget.total >= budget.policy.total ||
		entry.count >= budget.policy.perIP || !exists && len(budget.entries) >= budget.policy.keys {
		return nil, false
	}
	if !exists {
		entry.start = now
	}
	entry.count++
	budget.entries[key] = entry
	budget.total++
	budget.active++
	var once sync.Once
	return func() { once.Do(func() { budget.mu.Lock(); budget.active--; budget.mu.Unlock() }) }, true
}

func (budget *enrollmentBudget) prune(now time.Time) {
	if budget.start.IsZero() || now.Sub(budget.start) >= enrollmentBudgetWindow {
		budget.start, budget.total = now, 0
	}
	for key, entry := range budget.entries {
		if now.Sub(entry.start) >= enrollmentBudgetWindow {
			delete(budget.entries, key)
		}
	}
}
