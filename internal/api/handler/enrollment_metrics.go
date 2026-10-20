package handler

import (
	"net/http"
	"time"

	"github.com/beat/backend/internal/store"
)

// 入网协议结果的固定枚举：未知响应码统一归入 failed，避免出现无界标签。
const (
	enrollmentOutcomeServed      = "served"
	enrollmentOutcomeDenied      = "denied"
	enrollmentOutcomeRejected    = "rejected"
	enrollmentOutcomeLimited     = "limited"
	enrollmentOutcomeUnavailable = "unavailable"
	enrollmentOutcomeFailed      = "failed"
)

// EnrollmentMetrics 接收自动发现式入网的聚合计数与时延；未注入时全部观测为 no-op。
type EnrollmentMetrics interface {
	ObserveEnrollmentChallenge(outcome string)
	ObserveEnrollmentRequest(outcome string)
	ObserveEnrollmentPoll(outcome string)
	ObserveEnrollmentClaim(outcome string)
	ObserveEnrollmentRequestResume()
	ObserveEnrollmentApprovalLatency(time.Duration)
	ObserveEnrollmentClaimLatency(time.Duration)
}

// EnrollmentOutcomeForStatus 把协议响应码映射为固定结果枚举，供路由层统一计数。
func EnrollmentOutcomeForStatus(status int) string {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return enrollmentOutcomeServed
	case status == http.StatusForbidden:
		return enrollmentOutcomeDenied
	case status == http.StatusBadRequest:
		return enrollmentOutcomeRejected
	case status == http.StatusTooManyRequests:
		return enrollmentOutcomeLimited
	case status == http.StatusServiceUnavailable:
		return enrollmentOutcomeUnavailable
	default:
		return enrollmentOutcomeFailed
	}
}

// SetEnrollmentMetrics 注入入网观测器；nil 表示关闭观测。
func (handler *AgentEnrollmentAdminHandler) SetEnrollmentMetrics(metrics EnrollmentMetrics) {
	handler.metrics = metrics
}

// SetEnrollmentMetrics 注入入网观测器；nil 表示关闭观测。
func (handler *AgentDiscoveryHandler) SetEnrollmentMetrics(metrics EnrollmentMetrics) {
	handler.metrics = metrics
}

// observeApprovalLatency 记录人工审批的等待时长；缺少审批时间时视为无样本。
func (handler *AgentEnrollmentAdminHandler) observeApprovalLatency(request store.EnrollmentRequest) {
	if handler.metrics == nil || !request.ApprovedAt.Valid {
		return
	}
	handler.metrics.ObserveEnrollmentApprovalLatency(request.ApprovedAt.Time.Sub(request.FirstRequestAt))
}

// observeAutomaticApprovalLatency 记录自动审批的等待时长，语义与人工审批一致。
func (handler *AgentDiscoveryHandler) observeAutomaticApprovalLatency(request store.EnrollmentRequest) {
	if handler.metrics == nil || !request.ApprovedAt.Valid {
		return
	}
	handler.metrics.ObserveEnrollmentApprovalLatency(request.ApprovedAt.Time.Sub(request.FirstRequestAt))
}

// observeClaimLatency 记录从审批通过到凭据认领的时长；未审批过的请求不产生样本。
func (handler *AgentDiscoveryHandler) observeClaimLatency(approvedAt time.Time) {
	if handler.metrics == nil || approvedAt.IsZero() {
		return
	}
	handler.metrics.ObserveEnrollmentClaimLatency(handler.now().UTC().Sub(approvedAt))
}
