package serviceprincipal

import (
	"testing"
	"time"
)

func testLimiter(now *time.Time, limits RateLimits) *Limiter {
	limiter := NewLimiter(limits)
	limiter.now = func() time.Time { return *now }
	return limiter
}

func TestLimiterAllowsBurstThenDeniesWithRetry(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	limiter := testLimiter(&now, RateLimits{Token: RateLimit{Burst: 3, Interval: time.Second}, Source: RateLimit{Burst: 10, Interval: time.Second}})

	for attempt := 0; attempt < 3; attempt++ {
		if decision := limiter.Allow("token-one", "192.0.2.10"); !decision.Allowed {
			t.Fatalf("attempt %d denied: %+v", attempt, decision)
		}
	}
	denied := limiter.Allow("token-one", "192.0.2.10")
	if denied.Allowed || denied.Scope != RateLimitScopeToken {
		t.Fatalf("decision = %+v, want token denial", denied)
	}
	if denied.RetryAfter <= 0 || denied.RetryAfter > time.Second {
		t.Fatalf("retry after = %s, want within one second", denied.RetryAfter)
	}

	now = now.Add(time.Second)
	if decision := limiter.Allow("token-one", "192.0.2.10"); !decision.Allowed {
		t.Fatalf("refilled decision = %+v, want allowed", decision)
	}
}

func TestLimiterTracksTokenAndSourceSeparately(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	limiter := testLimiter(&now, RateLimits{Token: RateLimit{Burst: 50, Interval: time.Minute}, Source: RateLimit{Burst: 2, Interval: time.Minute}})

	if decision := limiter.Allow("token-one", "192.0.2.10"); !decision.Allowed {
		t.Fatalf("first decision = %+v", decision)
	}
	if decision := limiter.Allow("token-two", "192.0.2.10"); !decision.Allowed {
		t.Fatalf("second decision = %+v", decision)
	}
	denied := limiter.Allow("token-three", "192.0.2.10")
	if denied.Allowed || denied.Scope != RateLimitScopeSource {
		t.Fatalf("decision = %+v, want source denial", denied)
	}
	if decision := limiter.Allow("token-three", "192.0.2.11"); !decision.Allowed {
		t.Fatalf("other source decision = %+v, want allowed", decision)
	}
}

func TestLimiterSkipsUnknownSourceAndKeepsMemoryBounded(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	limiter := testLimiter(&now, RateLimits{Token: RateLimit{Burst: 1, Interval: time.Hour}, Source: RateLimit{Burst: 1, Interval: time.Hour}})
	limiter.maxEntries = 2

	for _, token := range []string{"token-one", "token-two", "token-three"} {
		if decision := limiter.Allow(token, ""); !decision.Allowed {
			t.Fatalf("unknown source decision = %+v, want allowed", decision)
		}
	}
	for _, token := range []string{"token-a", "token-b", "token-c", "token-d"} {
		now = now.Add(time.Second)
		if decision := limiter.Allow(token, ""); !decision.Allowed {
			t.Fatalf("token %s decision = %+v, want allowed", token, decision)
		}
	}
	if entries := limiter.Entries(); entries > 2 {
		t.Fatalf("entries = %d, want at most 2", entries)
	}
}

func TestLimiterFallsBackToDefaultLimits(t *testing.T) {
	limiter := NewLimiter(RateLimits{})
	defaults := DefaultRateLimits()
	if limiter.limits.Token != defaults.Token || limiter.limits.Source != defaults.Source {
		t.Fatalf("limits = %+v, want %+v", limiter.limits, defaults)
	}
}
