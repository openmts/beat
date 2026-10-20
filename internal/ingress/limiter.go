package ingress

import (
	"net/http"
	"sync"
	"time"
)

// pruneBatch 限制单次请求内的过期扫描量，避免清理成本随表大小增长。
const pruneBatch = 512

type identityKey struct {
	class Class
	value string
}

type identityState struct {
	bucket   bucket
	inFlight int
	seen     time.Time
}

// Limiter 维护全局与身份的令牌桶、在途租约和有界身份表。
type Limiter struct {
	mu              sync.Mutex
	capacity        Capacity
	clock           func() time.Time
	global          [classCount]bucket
	globalInFlight  [classCount]int
	identities      map[identityKey]*identityState
	admitted        [classCount]uint64
	rejected        [classCount]uint64
	rejectedReasons [classCount][reasonCount]uint64
}

// New 构造限流器；非法容量在启动期直接失败，不做静默降级。
func New(capacity Capacity) (*Limiter, error) {
	if err := capacity.Validate(); err != nil {
		return nil, err
	}
	return newLimiter(capacity, time.Now), nil
}

func newLimiter(capacity Capacity, clock func() time.Time) *Limiter {
	limiter := &Limiter{capacity: capacity, clock: clock, identities: map[identityKey]*identityState{}}
	now := clock()
	for _, class := range Classes() {
		limiter.global[class] = newBucket(capacity.Policies[class].GlobalRate, now)
	}
	return limiter
}

// Lease 是一次成功准入的租约；释放必须发生在请求边界，且可重复调用。
type Lease struct {
	limiter *Limiter
	key     identityKey
	once    *sync.Once
}

// Release 归还全局与身份在途预算。取消、失败、panic 与正常返回都要释放。
func (lease Lease) Release() {
	if lease.limiter == nil || lease.once == nil {
		return
	}
	lease.once.Do(func() {
		limiter := lease.limiter
		limiter.mu.Lock()
		defer limiter.mu.Unlock()
		if state, ok := limiter.identities[lease.key]; ok {
			state.inFlight = max(state.inFlight-1, 0)
			state.seen = limiter.clock()
		}
		class := lease.key.class
		limiter.globalInFlight[class] = max(limiter.globalInFlight[class]-1, 0)
	})
}

// Admit 先于口令散列、SQLite、MTS、通知与 SSH 等昂贵操作判定是否放行。
func (limiter *Limiter) Admit(class Class, identity string) (Lease, *Rejection) {
	if !class.Valid() {
		return Lease{}, nil
	}
	key := identityKey{class: class, value: identity}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock()
	limiter.pruneLocked(now)
	if rejection := limiter.admitLocked(key, now); rejection != nil {
		limiter.rejected[class]++
		limiter.rejectedReasons[class][rejection.Reason]++
		return Lease{}, rejection
	}
	limiter.admitted[class]++
	return Lease{limiter: limiter, key: key, once: &sync.Once{}}, nil
}

// admitLocked 依次消费全局在途、身份在途、全局速率与身份速率预算。
func (limiter *Limiter) admitLocked(key identityKey, now time.Time) *Rejection {
	class := key.class
	policy := limiter.capacity.Policies[class]
	if limiter.globalInFlight[class] >= policy.GlobalInFlight {
		return newRejection(ReasonClassBusy, http.StatusServiceUnavailable, classBusyRetryAfter)
	}
	state, available := limiter.identityLocked(key, policy, now)
	if !available {
		return newRejection(ReasonTableFull, http.StatusTooManyRequests, tableFullRetryAfter)
	}
	if state.inFlight >= policy.IdentityInFlight {
		return newRejection(ReasonIdentityBusy, http.StatusTooManyRequests, identityBusyRetryAfter)
	}
	global := &limiter.global[class]
	global.refill(policy.GlobalRate, now)
	state.bucket.refill(policy.IdentityRate, now)
	if global.tokens < 1 {
		return newRejection(ReasonClassRate, http.StatusTooManyRequests, global.retryAfter(policy.GlobalRate))
	}
	if state.bucket.tokens < 1 {
		return newRejection(ReasonIdentityRate, http.StatusTooManyRequests, state.bucket.retryAfter(policy.IdentityRate))
	}
	global.tokens--
	state.bucket.tokens--
	limiter.globalInFlight[class]++
	state.inFlight++
	state.seen = now
	return nil
}

// identityLocked 返回身份状态；表满且出现新身份时在全局类别预算下失败关闭。
func (limiter *Limiter) identityLocked(key identityKey, policy Policy, now time.Time) (*identityState, bool) {
	if state, ok := limiter.identities[key]; ok {
		return state, true
	}
	if len(limiter.identities) >= limiter.capacity.IdentityEntries {
		return nil, false
	}
	state := &identityState{bucket: newBucket(policy.IdentityRate, now), seen: now}
	limiter.identities[key] = state
	return state, true
}

// pruneLocked 先删除过期条目；活动租约永远不会被提前驱逐。
func (limiter *Limiter) pruneLocked(now time.Time) {
	deadline := now.Add(-limiter.capacity.IdentityTTL)
	scanned := 0
	for key, state := range limiter.identities {
		if state.inFlight > 0 || state.seen.After(deadline) {
			continue
		}
		delete(limiter.identities, key)
		scanned++
		if scanned >= pruneBatch {
			return
		}
	}
}
