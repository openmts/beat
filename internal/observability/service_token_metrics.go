package observability

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const serviceTokenStateActive, serviceTokenStateExpiring, serviceTokenStateRevoked = "active", "expiring", "revoked"

// 服务令牌指标只接受固定枚举标签，避免高基数序列。
var (
	serviceTokenAuthOutcomes  = []string{"authenticated", "denied", "unavailable"}
	serviceTokenDenialReasons = []string{"scope", "resource", "visibility", "rate_limit"}
	serviceTokenLimitScopes   = []string{"token", "source"}
	serviceTokenStates        = []string{serviceTokenStateActive, serviceTokenStateExpiring, serviceTokenStateRevoked}
	serviceTokenScopeLabels   = serviceTokenScopeSet()
)

// ServiceTokenStatistics 是抓取时的服务令牌计数快照。
type ServiceTokenStatistics struct {
	Active   uint64
	Expiring uint64
	Revoked  uint64
}

type serviceTokenSnapshot struct {
	auth      map[string]uint64
	denials   map[string]uint64
	limits    map[string]uint64
	latencies map[string]requestStats
	stats     ServiceTokenStatistics
}

// SetServiceTokenStatistics 注册计数来源；抓取失败时保留上一次成功的快照。
func (registry *Registry) SetServiceTokenStatistics(source func() (ServiceTokenStatistics, error)) {
	registry.mu.Lock()
	registry.statistics = source
	registry.mu.Unlock()
}

// ObserveServiceTokenAuth 记录令牌认证结果；未知取值被忽略。
func (registry *Registry) ObserveServiceTokenAuth(outcome string) {
	registry.count(registry.tokenAuth, outcome, serviceTokenAuthOutcomes)
}

// ObserveServiceTokenDenial 记录授权拒绝原因；未知取值被忽略。
func (registry *Registry) ObserveServiceTokenDenial(reason string) {
	registry.count(registry.denials, reason, serviceTokenDenialReasons)
}

// ObserveServiceTokenRateLimit 记录触发限流的维度；未知取值被忽略。
func (registry *Registry) ObserveServiceTokenRateLimit(scope string) {
	registry.count(registry.limits, scope, serviceTokenLimitScopes)
}

// ObserveServiceTokenLatency 记录数据面请求时延；scope 必须是固定权限枚举。
func (registry *Registry) ObserveServiceTokenLatency(scope string, duration time.Duration) {
	if !serviceTokenScopeLabels[scope] {
		return
	}
	seconds := duration.Seconds()
	registry.mu.Lock()
	stats := registry.latencies[scope]
	if stats == nil {
		stats = &requestStats{buckets: make([]uint64, len(latencyBuckets))}
		registry.latencies[scope] = stats
	}
	stats.count++
	stats.sum += seconds
	for index, boundary := range latencyBuckets {
		if seconds <= boundary {
			stats.buckets[index]++
		}
	}
	registry.mu.Unlock()
}

func (registry *Registry) count(target map[string]uint64, label string, allowed []string) {
	if !slices.Contains(allowed, label) {
		return
	}
	registry.mu.Lock()
	target[label]++
	registry.mu.Unlock()
}

// refreshServiceTokenStatistics 在与注册表锁无关的前提下刷新计数快照。
func (registry *Registry) refreshServiceTokenStatistics() ServiceTokenStatistics {
	registry.mu.RLock()
	source := registry.statistics
	registry.mu.RUnlock()
	if source != nil {
		if stats, err := source(); err == nil {
			registry.mu.Lock()
			registry.tokenStats = stats
			registry.mu.Unlock()
		}
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.tokenStats
}

func cloneCounters(source map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneLatencies(source map[string]*requestStats) map[string]requestStats {
	result := make(map[string]requestStats, len(source))
	for key, stats := range source {
		copyStats := *stats
		copyStats.buckets = append([]uint64(nil), stats.buckets...)
		result[key] = copyStats
	}
	return result
}

func writeServiceTokenMetrics(output *strings.Builder, snapshot serviceTokenSnapshot) {
	output.WriteString("# TYPE beat_service_token_authentications_total counter\n")
	for _, outcome := range serviceTokenAuthOutcomes {
		fmt.Fprintf(output, "beat_service_token_authentications_total{outcome=%s} %d\n", quoteLabel(outcome), snapshot.auth[outcome])
	}
	output.WriteString("# TYPE beat_service_token_denials_total counter\n")
	for _, reason := range serviceTokenDenialReasons {
		fmt.Fprintf(output, "beat_service_token_denials_total{reason=%s} %d\n", quoteLabel(reason), snapshot.denials[reason])
	}
	output.WriteString("# TYPE beat_service_token_rate_limits_total counter\n")
	for _, scope := range serviceTokenLimitScopes {
		fmt.Fprintf(output, "beat_service_token_rate_limits_total{kind=%s} %d\n", quoteLabel(scope), snapshot.limits[scope])
	}
	writeServiceTokenLatency(output, snapshot.latencies)
	output.WriteString("# TYPE beat_service_tokens gauge\n")
	for _, state := range serviceTokenStates {
		fmt.Fprintf(output, "beat_service_tokens{state=%s} %d\n", quoteLabel(state), snapshot.stats.valueOf(state))
	}
}

func writeServiceTokenLatency(output *strings.Builder, latencies map[string]requestStats) {
	output.WriteString("# TYPE beat_service_token_operation_seconds histogram\n")
	scopes := make([]string, 0, len(latencies))
	for scope := range latencies {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	for _, scope := range scopes {
		stats := latencies[scope]
		labels := "scope=" + quoteLabel(scope)
		for index, boundary := range latencyBuckets {
			fmt.Fprintf(output, "beat_service_token_operation_seconds_bucket{%s,le=%q} %d\n",
				labels, strconv.FormatFloat(boundary, 'g', -1, 64), stats.buckets[index])
		}
		fmt.Fprintf(output, "beat_service_token_operation_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, stats.count)
		fmt.Fprintf(output, "beat_service_token_operation_seconds_sum{%s} %g\n", labels, stats.sum)
		fmt.Fprintf(output, "beat_service_token_operation_seconds_count{%s} %d\n", labels, stats.count)
	}
}

func (statistics ServiceTokenStatistics) valueOf(state string) uint64 {
	switch state {
	case serviceTokenStateActive:
		return statistics.Active
	case serviceTokenStateExpiring:
		return statistics.Expiring
	default:
		return statistics.Revoked
	}
}

func serviceTokenScopeSet() map[string]bool {
	scopes := make(map[string]bool, len(model.ServiceScopeCatalog))
	for _, scope := range model.ServiceScopeCatalog {
		scopes[string(scope)] = true
	}
	return scopes
}
