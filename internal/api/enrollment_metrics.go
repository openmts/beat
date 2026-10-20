package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
)

// 入网协议的四条路径分开计数，结果由响应码映射为固定枚举。
const (
	enrollmentFamilyChallenge = "challenge"
	enrollmentFamilyRequest   = "request"
	enrollmentFamilyPoll      = "poll"
	enrollmentFamilyClaim     = "claim"
)

// observeEnrollment 在协议处理结束后记录一次结果；未注入观测器时直接透传。
func (r *Router) observeEnrollment(family string, next http.Handler) http.Handler {
	metrics := r.enrollmentMetrics
	if metrics == nil {
		return next
	}
	return middleware.ObserveStatus(next, func(status int) {
		outcome := handler.EnrollmentOutcomeForStatus(status)
		switch family {
		case enrollmentFamilyChallenge:
			metrics.ObserveEnrollmentChallenge(outcome)
		case enrollmentFamilyRequest:
			metrics.ObserveEnrollmentRequest(outcome)
		case enrollmentFamilyPoll:
			metrics.ObserveEnrollmentPoll(outcome)
		case enrollmentFamilyClaim:
			metrics.ObserveEnrollmentClaim(outcome)
		}
	})
}
