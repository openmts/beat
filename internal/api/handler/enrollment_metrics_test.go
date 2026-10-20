package handler

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

type recordingEnrollmentMetrics struct {
	challenges, requests, polls, claims []string
	approvals, claimLatencies           []time.Duration
	resumes                             int
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentChallenge(outcome string) {
	metrics.challenges = append(metrics.challenges, outcome)
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentRequest(outcome string) {
	metrics.requests = append(metrics.requests, outcome)
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentPoll(outcome string) {
	metrics.polls = append(metrics.polls, outcome)
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentClaim(outcome string) {
	metrics.claims = append(metrics.claims, outcome)
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentRequestResume() { metrics.resumes++ }

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentApprovalLatency(duration time.Duration) {
	metrics.approvals = append(metrics.approvals, duration)
}

func (metrics *recordingEnrollmentMetrics) ObserveEnrollmentClaimLatency(duration time.Duration) {
	metrics.claimLatencies = append(metrics.claimLatencies, duration)
}

func TestEnrollmentOutcomeForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusOK, enrollmentOutcomeServed},
		{http.StatusCreated, enrollmentOutcomeServed},
		{http.StatusAccepted, enrollmentOutcomeServed},
		{http.StatusForbidden, enrollmentOutcomeDenied},
		{http.StatusBadRequest, enrollmentOutcomeRejected},
		{http.StatusTooManyRequests, enrollmentOutcomeLimited},
		{http.StatusServiceUnavailable, enrollmentOutcomeUnavailable},
		{http.StatusNotFound, enrollmentOutcomeFailed},
		{http.StatusInternalServerError, enrollmentOutcomeFailed},
	}
	for _, testCase := range cases {
		if got := EnrollmentOutcomeForStatus(testCase.status); got != testCase.want {
			t.Fatalf("status %d mapped to %q, want %q", testCase.status, got, testCase.want)
		}
	}
}

func TestEnrollmentLatencyObservations(t *testing.T) {
	metrics := &recordingEnrollmentMetrics{}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	discovery := NewAgentDiscoveryHandler(nil, nil, false)
	discovery.now = func() time.Time { return now }
	discovery.SetEnrollmentMetrics(metrics)
	discovery.observeAutomaticApprovalLatency(store.EnrollmentRequest{
		ApprovedAt:     sql.NullTime{Time: now.Add(-5 * time.Minute), Valid: true},
		FirstRequestAt: now.Add(-12 * time.Minute)})
	discovery.observeAutomaticApprovalLatency(store.EnrollmentRequest{FirstRequestAt: now.Add(-time.Hour)})
	discovery.observeClaimLatency(now.Add(-2 * time.Minute))
	discovery.observeClaimLatency(time.Time{})

	admin := NewAgentEnrollmentAdminHandler(nil)
	admin.SetEnrollmentMetrics(metrics)
	admin.observeApprovalLatency(store.EnrollmentRequest{
		ApprovedAt: sql.NullTime{Time: now, Valid: true}, FirstRequestAt: now.Add(-time.Hour)})
	admin.observeApprovalLatency(store.EnrollmentRequest{FirstRequestAt: now.Add(-time.Hour)})

	if len(metrics.approvals) != 2 || metrics.approvals[0] != 7*time.Minute || metrics.approvals[1] != time.Hour {
		t.Fatalf("approval samples = %v", metrics.approvals)
	}
	if len(metrics.claimLatencies) != 1 || metrics.claimLatencies[0] != 2*time.Minute {
		t.Fatalf("claim samples = %v", metrics.claimLatencies)
	}
}

func TestEnrollmentObservationsWithoutMetrics(t *testing.T) {
	now := time.Now().UTC()
	discovery := NewAgentDiscoveryHandler(nil, nil, false)
	discovery.observeClaimLatency(now)
	discovery.observeAutomaticApprovalLatency(store.EnrollmentRequest{
		ApprovedAt: sql.NullTime{Time: now, Valid: true}, FirstRequestAt: now})
	admin := NewAgentEnrollmentAdminHandler(nil)
	admin.observeApprovalLatency(store.EnrollmentRequest{
		ApprovedAt: sql.NullTime{Time: now, Valid: true}, FirstRequestAt: now})
}
