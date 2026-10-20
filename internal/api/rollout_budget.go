package api

import (
	"sync"
	"time"
)

const rolloutKeyRetention = time.Minute

const rolloutMaximumKeys = 4096

type rolloutBudgetPolicy struct {
	burst, rate             float64
	globalBurst, globalRate float64
	concurrent              int
}

type rolloutBucket struct {
	tokens   float64
	at, last time.Time
}

type rolloutBudget struct {
	mu      sync.Mutex
	policy  rolloutBudgetPolicy
	entries map[string]rolloutBucket
	global  rolloutBucket
	active  int
}

func newRolloutBudget(policy rolloutBudgetPolicy) *rolloutBudget {
	return &rolloutBudget{policy: policy, entries: map[string]rolloutBucket{}}
}

func (bucket rolloutBucket) refill(now time.Time, burst, rate float64) rolloutBucket {
	if bucket.at.IsZero() {
		return rolloutBucket{tokens: burst, at: now, last: now}
	}
	elapsed := now.Sub(bucket.at).Seconds()
	if elapsed > 0 {
		bucket.tokens = min(burst, bucket.tokens+elapsed*rate)
		bucket.at = now
	}
	if now.After(bucket.last) {
		bucket.last = now
	}
	return bucket
}

func (b *rolloutBudget) acquire(key string, now time.Time) (func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, entry := range b.entries {
		if now.Sub(entry.last) >= rolloutKeyRetention {
			delete(b.entries, key)
		}
	}
	entry, exists := b.entries[key]
	if b.active >= b.policy.concurrent || !exists && len(b.entries) >= rolloutMaximumKeys {
		return nil, false
	}
	b.global = b.global.refill(now, b.policy.globalBurst, b.policy.globalRate)
	entry = entry.refill(now, b.policy.burst, b.policy.rate)
	if b.global.tokens < 1 || entry.tokens < 1 {
		return nil, false
	}
	b.global.tokens--
	entry.tokens--
	b.entries[key] = entry
	b.active++
	var once sync.Once
	return func() { once.Do(func() { b.mu.Lock(); b.active--; b.mu.Unlock() }) }, true
}
