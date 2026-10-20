package serviceprincipal

import (
	"sync"
	"time"
)

// RateLimit 描述单个维度（令牌或来源）的令牌桶参数。
type RateLimit struct {
	Burst    int
	Interval time.Duration
}

// RateLimits 组合令牌维度与来源维度的限流参数。
type RateLimits struct {
	Token  RateLimit
	Source RateLimit
}

const (
	defaultRateLimitEntries = 4096
	// RateLimitScopeToken 与 RateLimitScopeSource 是限流指标的有界标签取值。
	RateLimitScopeToken  = "token"
	RateLimitScopeSource = "source"
)

// DefaultRateLimits 返回服务令牌的默认限流参数：单令牌每秒 60 次、单来源每秒 240 次。
func DefaultRateLimits() RateLimits {
	return RateLimits{
		Token:  RateLimit{Burst: 60, Interval: time.Second},
		Source: RateLimit{Burst: 240, Interval: time.Second},
	}
}

// RateDecision 是一次限流判定结果；Scope 只取有界枚举值。
type RateDecision struct {
	Allowed    bool
	Scope      string
	RetryAfter time.Duration
}

type rateBucket struct {
	available float64
	updated   time.Time
}

// Limiter 对令牌与来源分别做有界令牌桶限流，超过容量时淘汰最久未更新的键。
type Limiter struct {
	mu         sync.Mutex
	limits     RateLimits
	maxEntries int
	now        func() time.Time
	tokens     map[string]rateBucket
	sources    map[string]rateBucket
}

// NewLimiter 创建限流器；参数非法时使用默认值。
func NewLimiter(limits RateLimits) *Limiter {
	if limits.Token.Burst <= 0 || limits.Token.Interval <= 0 {
		limits.Token = DefaultRateLimits().Token
	}
	if limits.Source.Burst <= 0 || limits.Source.Interval <= 0 {
		limits.Source = DefaultRateLimits().Source
	}
	return &Limiter{
		limits: limits, maxEntries: defaultRateLimitEntries,
		now:    func() time.Time { return time.Now() },
		tokens: map[string]rateBucket{}, sources: map[string]rateBucket{},
	}
}

// Allow 同时判定令牌与来源两个桶；两个桶都通过才消费额度。
func (limiter *Limiter) Allow(tokenID, source string) RateDecision {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	tokenDecision := limiter.preview(limiter.tokens, tokenID, limiter.limits.Token, now)
	if !tokenDecision.Allowed {
		return tokenDecision
	}
	sourceDecision := limiter.preview(limiter.sources, source, limiter.limits.Source, now)
	if !sourceDecision.Allowed {
		return sourceDecision
	}
	limiter.consume(limiter.tokens, tokenID, limiter.limits.Token, now)
	limiter.consume(limiter.sources, source, limiter.limits.Source, now)
	return RateDecision{Allowed: true}
}

// Entries 返回当前跟踪的键数量，用于容量断言与运维观测。
func (limiter *Limiter) Entries() int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.tokens) + len(limiter.sources)
}

// preview 在不修改状态的前提下计算补充后的可用额度。
func (limiter *Limiter) preview(buckets map[string]rateBucket, key string, limit RateLimit, now time.Time) RateDecision {
	if key == "" {
		return RateDecision{Allowed: true}
	}
	available := limiter.refill(buckets[key], limit, now)
	if available >= 1 {
		return RateDecision{Allowed: true}
	}
	wait := time.Duration((1 - available) * float64(limit.Interval))
	return RateDecision{Allowed: false, Scope: scopeOf(limit, limiter.limits), RetryAfter: wait}
}

// consume 扣减一个额度并写入桶状态，必要时淘汰最旧条目。
func (limiter *Limiter) consume(buckets map[string]rateBucket, key string, limit RateLimit, now time.Time) {
	if key == "" {
		return
	}
	if _, present := buckets[key]; !present && len(buckets) >= limiter.maxEntries {
		evictOldest(buckets)
	}
	available := limiter.refill(buckets[key], limit, now)
	buckets[key] = rateBucket{available: available - 1, updated: now}
}

func (limiter *Limiter) refill(bucket rateBucket, limit RateLimit, now time.Time) float64 {
	available := bucket.available
	if bucket.updated.IsZero() {
		available = float64(limit.Burst)
	}
	available += now.Sub(bucket.updated).Seconds() / limit.Interval.Seconds()
	if available > float64(limit.Burst) {
		available = float64(limit.Burst)
	}
	return available
}

func scopeOf(limit RateLimit, limits RateLimits) string {
	if limit == limits.Token {
		return RateLimitScopeToken
	}
	return RateLimitScopeSource
}

func evictOldest(buckets map[string]rateBucket) {
	var oldestKey string
	var oldest time.Time
	for key, bucket := range buckets {
		if oldestKey == "" || bucket.updated.Before(oldest) {
			oldestKey, oldest = key, bucket.updated
		}
	}
	delete(buckets, oldestKey)
}
