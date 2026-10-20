package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type recordingRouterMetrics struct {
	challenges, requests, polls, claims []string
}

func (metrics *recordingRouterMetrics) ObserveEnrollmentChallenge(outcome string) {
	metrics.challenges = append(metrics.challenges, outcome)
}

func (metrics *recordingRouterMetrics) ObserveEnrollmentRequest(outcome string) {
	metrics.requests = append(metrics.requests, outcome)
}

func (metrics *recordingRouterMetrics) ObserveEnrollmentPoll(outcome string) {
	metrics.polls = append(metrics.polls, outcome)
}

func (metrics *recordingRouterMetrics) ObserveEnrollmentClaim(outcome string) {
	metrics.claims = append(metrics.claims, outcome)
}

func (metrics *recordingRouterMetrics) ObserveEnrollmentRequestResume() {}

func (metrics *recordingRouterMetrics) ObserveEnrollmentApprovalLatency(time.Duration) {}

func (metrics *recordingRouterMetrics) ObserveEnrollmentClaimLatency(time.Duration) {}

func TestObserveEnrollmentRecordsPerFamilyOutcome(t *testing.T) {
	metrics := &recordingRouterMetrics{}
	router := &Router{enrollmentMetrics: metrics}
	cases := []struct {
		family string
		status int
		count  func() int
	}{
		{enrollmentFamilyChallenge, http.StatusCreated, func() int { return len(metrics.challenges) }},
		{enrollmentFamilyRequest, http.StatusForbidden, func() int { return len(metrics.requests) }},
		{enrollmentFamilyPoll, http.StatusTooManyRequests, func() int { return len(metrics.polls) }},
		{enrollmentFamilyClaim, http.StatusServiceUnavailable, func() int { return len(metrics.claims) }},
	}
	for _, testCase := range cases {
		handler := router.observeEnrollment(testCase.family, statusHandler(testCase.status))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
		if testCase.count() != 1 {
			t.Fatalf("family %s recorded %d observations", testCase.family, testCase.count())
		}
	}
	if metrics.challenges[0] != "served" || metrics.requests[0] != "denied" ||
		metrics.polls[0] != "limited" || metrics.claims[0] != "unavailable" {
		t.Fatalf("outcomes = %#v", metrics)
	}

	unknown := router.observeEnrollment("readiness", statusHandler(http.StatusOK))
	unknown.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	if len(metrics.challenges)+len(metrics.requests)+len(metrics.polls)+len(metrics.claims) != 4 {
		t.Fatal("unknown family recorded an observation")
	}
}

func TestObserveEnrollmentWithoutMetricsPassesThrough(t *testing.T) {
	router := &Router{}
	handler := router.observeEnrollment(enrollmentFamilyChallenge, statusHandler(http.StatusCreated))
	if handler != nil {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	}
}

func statusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
}
