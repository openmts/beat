package adminauth

import (
	"sync"
	"time"
)

const (
	loginWindow   = 15 * time.Minute
	loginLockout  = 15 * time.Minute
	loginFailures = 5
)

type rateEntry struct {
	windowStart time.Time
	failures    int
	lockedUntil time.Time
}

type RateLimiter struct {
	mu         sync.Mutex
	entries    map[string]rateEntry
	maxEntries int
}

func NewRateLimiter(maxEntries int) *RateLimiter {
	if maxEntries < 64 {
		maxEntries = 64
	}
	return &RateLimiter{entries: make(map[string]rateEntry), maxEntries: maxEntries}
}

func (limiter *RateLimiter) Allowed(keys []string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.prune(now)
	for _, key := range keys {
		if entry, ok := limiter.entries[key]; ok && now.Before(entry.lockedUntil) {
			return false
		}
	}
	return true
}

func (limiter *RateLimiter) Failure(keys []string, now time.Time) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.prune(now)
	for _, key := range keys {
		entry := limiter.entries[key]
		if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= loginWindow {
			entry = rateEntry{windowStart: now}
		}
		entry.failures++
		if entry.failures >= loginFailures {
			entry.lockedUntil = now.Add(loginLockout)
		}
		limiter.entries[key] = entry
	}
}

func (limiter *RateLimiter) Success(keys []string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	for _, key := range keys {
		delete(limiter.entries, key)
	}
}

func (limiter *RateLimiter) prune(now time.Time) {
	for key, entry := range limiter.entries {
		if !entry.lockedUntil.IsZero() && now.After(entry.lockedUntil) || now.Sub(entry.windowStart) > 2*loginWindow {
			delete(limiter.entries, key)
		}
	}
	for len(limiter.entries) >= limiter.maxEntries {
		for key := range limiter.entries {
			delete(limiter.entries, key)
			break
		}
	}
}
