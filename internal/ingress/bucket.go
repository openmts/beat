package ingress

import (
	"math"
	"time"
)

// bucket 是标准令牌桶：按经过时间补充令牌，最多补到突发上限。
type bucket struct {
	tokens  float64
	updated time.Time
}

func newBucket(rate Rate, now time.Time) bucket {
	return bucket{tokens: float64(rate.Burst), updated: now}
}

func (value *bucket) refill(rate Rate, now time.Time) {
	elapsed := now.Sub(value.updated).Seconds()
	if elapsed <= 0 {
		return
	}
	value.updated = now
	value.tokens = math.Min(float64(rate.Burst), value.tokens+elapsed*rate.PerSecond)
}

// retryAfter 返回下一次可用的整秒等待时间，始终在 [1, maximumRetryAfter] 内。
func (value *bucket) retryAfter(rate Rate) int {
	missing := 1 - value.tokens
	if missing <= 0 {
		return 1
	}
	seconds := int(math.Ceil(missing / rate.PerSecond))
	if seconds < 1 {
		return 1
	}
	return min(seconds, maximumRetryAfter)
}
