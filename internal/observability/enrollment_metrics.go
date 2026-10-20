package observability

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 入网指标只接受固定结果枚举：协议结果、请求状态、授权状态与清理结果。
// 授权 ID、请求 ID、节点、主机名、来源地址与任何秘密字段永远不会成为标签。
const (
	enrollmentOutcomeServed      = "served"
	enrollmentOutcomeDenied      = "denied"
	enrollmentOutcomeRejected    = "rejected"
	enrollmentOutcomeLimited     = "limited"
	enrollmentOutcomeUnavailable = "unavailable"
	enrollmentOutcomeFailed      = "failed"

	enrollmentCleanupCompleted = "completed"
	enrollmentCleanupFailed    = "failed"
)

var (
	enrollmentOutcomes = []string{
		enrollmentOutcomeServed, enrollmentOutcomeDenied, enrollmentOutcomeRejected,
		enrollmentOutcomeLimited, enrollmentOutcomeUnavailable, enrollmentOutcomeFailed,
	}
	enrollmentCleanupResults = []string{enrollmentCleanupCompleted, enrollmentCleanupFailed}
	// enrollmentRequestStates 与 agentenrollment.RequestStatus 的白名单逐值对齐。
	enrollmentRequestStates = []string{"pending", "approved", "rejected", "expired", "claimed"}
	enrollmentGrantStates   = []string{"active", "expired", "disabled", "revoked"}
)

// EnrollmentStatistics 是抓取时的入网聚合快照：请求状态计数、最老未决请求年龄与授权状态计数。
type EnrollmentStatistics struct {
	Pending, Approved, Rejected, Expired, Claimed              uint64
	OldestPendingAge                                           time.Duration
	ActiveGrants, ExpiredGrants, DisabledGrants, RevokedGrants uint64
}

type enrollmentSnapshot struct {
	challenges map[string]uint64
	requests   map[string]uint64
	polls      map[string]uint64
	claims     map[string]uint64
	cleanup    map[string]uint64
	resumes    uint64
	approval   *requestStats
	claim      *requestStats
	statistics EnrollmentStatistics
}

// SetEnrollmentStatistics 注册入网聚合来源；抓取失败时保留上一次成功的快照。
func (registry *Registry) SetEnrollmentStatistics(source func() (EnrollmentStatistics, error)) {
	registry.mu.Lock()
	registry.enrollmentStatistics = source
	registry.mu.Unlock()
}

// ObserveEnrollmentChallenge 记录挑战发放结果；未知取值被忽略。
func (registry *Registry) ObserveEnrollmentChallenge(outcome string) {
	registry.count(registry.enrollmentChallenges, outcome, enrollmentOutcomes)
}

// ObserveEnrollmentRequest 记录建请求或恢复请求的结果；未知取值被忽略。
func (registry *Registry) ObserveEnrollmentRequest(outcome string) {
	registry.count(registry.enrollmentRequests, outcome, enrollmentOutcomes)
}

// ObserveEnrollmentPoll 记录状态轮询结果；未知取值被忽略。
func (registry *Registry) ObserveEnrollmentPoll(outcome string) {
	registry.count(registry.enrollmentPolls, outcome, enrollmentOutcomes)
}

// ObserveEnrollmentClaim 记录凭据认领结果；未知取值被忽略。
func (registry *Registry) ObserveEnrollmentClaim(outcome string) {
	registry.count(registry.enrollmentClaims, outcome, enrollmentOutcomes)
}

// ObserveEnrollmentCleanup 记录清理回合结果；未知取值被忽略。
func (registry *Registry) ObserveEnrollmentCleanup(result string) {
	registry.count(registry.enrollmentCleanup, result, enrollmentCleanupResults)
}

// ObserveEnrollmentRequestResume 记录复用未认领请求的次数，供容量与重试分析。
func (registry *Registry) ObserveEnrollmentRequestResume() {
	registry.mu.Lock()
	registry.enrollmentResumes++
	registry.mu.Unlock()
}

// ObserveEnrollmentApprovalLatency 记录从首次建请求到审批通过的等待时长。
func (registry *Registry) ObserveEnrollmentApprovalLatency(duration time.Duration) {
	registry.observeEnrollmentLatency(&registry.enrollmentApproval, duration)
}

// ObserveEnrollmentClaimLatency 记录从审批通过到凭据认领的等待时长。
func (registry *Registry) ObserveEnrollmentClaimLatency(duration time.Duration) {
	registry.observeEnrollmentLatency(&registry.enrollmentClaim, duration)
}

// observeEnrollmentLatency 复用 HTTP 时延分桶；负值来自时钟回拨，直接丢弃。
func (registry *Registry) observeEnrollmentLatency(target **requestStats, duration time.Duration) {
	if duration < 0 {
		return
	}
	seconds := duration.Seconds()
	registry.mu.Lock()
	stats := *target
	if stats == nil {
		stats = &requestStats{buckets: make([]uint64, len(latencyBuckets))}
		*target = stats
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

// refreshEnrollmentStatistics 在与注册表锁无关的前提下刷新聚合快照。
func (registry *Registry) refreshEnrollmentStatistics() EnrollmentStatistics {
	registry.mu.RLock()
	source := registry.enrollmentStatistics
	registry.mu.RUnlock()
	if source == nil {
		return registry.enrollmentStatisticsSnapshot()
	}
	statistics, err := source()
	if err != nil {
		return registry.enrollmentStatisticsSnapshot()
	}
	registry.mu.Lock()
	registry.enrollmentStats = statistics
	registry.mu.Unlock()
	return statistics
}

func (registry *Registry) enrollmentStatisticsSnapshot() EnrollmentStatistics {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.enrollmentStats
}

func (registry *Registry) cloneEnrollmentSnapshot() enrollmentSnapshot {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return enrollmentSnapshot{
		challenges: cloneCounters(registry.enrollmentChallenges),
		requests:   cloneCounters(registry.enrollmentRequests),
		polls:      cloneCounters(registry.enrollmentPolls),
		claims:     cloneCounters(registry.enrollmentClaims),
		cleanup:    cloneCounters(registry.enrollmentCleanup),
		resumes:    registry.enrollmentResumes,
		approval:   cloneEnrollmentLatency(registry.enrollmentApproval),
		claim:      cloneEnrollmentLatency(registry.enrollmentClaim),
	}
}

func cloneEnrollmentLatency(stats *requestStats) *requestStats {
	if stats == nil {
		return nil
	}
	copied := *stats
	copied.buckets = append([]uint64(nil), stats.buckets...)
	return &copied
}

func writeEnrollmentMetrics(output *strings.Builder, snapshot enrollmentSnapshot) {
	writeEnrollmentCounter(output, "beat_agent_enrollment_challenges_total", "outcome", enrollmentOutcomes, snapshot.challenges)
	writeEnrollmentCounter(output, "beat_agent_enrollment_requests_total", "outcome", enrollmentOutcomes, snapshot.requests)
	writeEnrollmentCounter(output, "beat_agent_enrollment_polls_total", "outcome", enrollmentOutcomes, snapshot.polls)
	writeEnrollmentCounter(output, "beat_agent_enrollment_claims_total", "outcome", enrollmentOutcomes, snapshot.claims)
	writeEnrollmentCounter(output, "beat_agent_enrollment_cleanup_total", "result", enrollmentCleanupResults, snapshot.cleanup)
	output.WriteString("# TYPE beat_agent_enrollment_request_resumes_total counter\n")
	fmt.Fprintf(output, "beat_agent_enrollment_request_resumes_total %d\n", snapshot.resumes)
	output.WriteString("# TYPE beat_agent_enrollment_requests gauge\n")
	for _, status := range enrollmentRequestStates {
		fmt.Fprintf(output, "beat_agent_enrollment_requests{status=%s} %d\n",
			quoteLabel(status), snapshot.statistics.requestState(status))
	}
	output.WriteString("# TYPE beat_agent_enrollment_pending_age_seconds gauge\n")
	fmt.Fprintf(output, "beat_agent_enrollment_pending_age_seconds %g\n", snapshot.statistics.OldestPendingAge.Seconds())
	output.WriteString("# TYPE beat_agent_enrollment_grants gauge\n")
	for _, state := range enrollmentGrantStates {
		fmt.Fprintf(output, "beat_agent_enrollment_grants{state=%s} %d\n",
			quoteLabel(state), snapshot.statistics.grantState(state))
	}
	writeEnrollmentHistogram(output, "beat_agent_enrollment_approval_seconds", snapshot.approval)
	writeEnrollmentHistogram(output, "beat_agent_enrollment_claim_seconds", snapshot.claim)
}

func writeEnrollmentCounter(output *strings.Builder, name, label string, allowed []string, values map[string]uint64) {
	fmt.Fprintf(output, "# TYPE %s counter\n", name)
	for _, value := range allowed {
		fmt.Fprintf(output, "%s{%s=%s} %d\n", name, label, quoteLabel(value), values[value])
	}
}

func writeEnrollmentHistogram(output *strings.Builder, name string, stats *requestStats) {
	if stats == nil {
		return
	}
	fmt.Fprintf(output, "# TYPE %s histogram\n", name)
	for index, boundary := range latencyBuckets {
		fmt.Fprintf(output, "%s_bucket{le=%q} %d\n", name, strconv.FormatFloat(boundary, 'g', -1, 64), stats.buckets[index])
	}
	fmt.Fprintf(output, "%s_bucket{le=\"+Inf\"} %d\n", name, stats.count)
	fmt.Fprintf(output, "%s_sum %g\n", name, stats.sum)
	fmt.Fprintf(output, "%s_count %d\n", name, stats.count)
}

func (statistics EnrollmentStatistics) requestState(status string) uint64 {
	switch status {
	case "pending":
		return statistics.Pending
	case "approved":
		return statistics.Approved
	case "rejected":
		return statistics.Rejected
	case "expired":
		return statistics.Expired
	default:
		return statistics.Claimed
	}
}

func (statistics EnrollmentStatistics) grantState(state string) uint64 {
	switch state {
	case "active":
		return statistics.ActiveGrants
	case "expired":
		return statistics.ExpiredGrants
	case "disabled":
		return statistics.DisabledGrants
	default:
		return statistics.RevokedGrants
	}
}
