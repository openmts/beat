package observability

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistryEnrollmentCountersAndLatency(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveEnrollmentChallenge("served")
	registry.ObserveEnrollmentChallenge("served")
	registry.ObserveEnrollmentChallenge("nonsense")
	registry.ObserveEnrollmentRequest("rejected")
	registry.ObserveEnrollmentPoll("limited")
	registry.ObserveEnrollmentClaim("denied")
	registry.ObserveEnrollmentCleanup("completed")
	registry.ObserveEnrollmentCleanup("bogus")
	registry.ObserveEnrollmentRequestResume()
	registry.ObserveEnrollmentApprovalLatency(90 * time.Minute)
	registry.ObserveEnrollmentClaimLatency(-time.Second)
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_agent_enrollment_challenges_total{outcome="served"} 2`,
		`beat_agent_enrollment_challenges_total{outcome="failed"} 0`,
		`beat_agent_enrollment_requests_total{outcome="rejected"} 1`,
		`beat_agent_enrollment_polls_total{outcome="limited"} 1`,
		`beat_agent_enrollment_claims_total{outcome="denied"} 1`,
		`beat_agent_enrollment_cleanup_total{result="completed"} 1`,
		`beat_agent_enrollment_cleanup_total{result="failed"} 0`,
		"beat_agent_enrollment_request_resumes_total 1",
		"beat_agent_enrollment_approval_seconds_sum 5400",
		"beat_agent_enrollment_approval_seconds_count 1",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
	if strings.Contains(metrics, `outcome="nonsense"`) {
		t.Fatalf("unbounded outcome label was kept:\n%s", metrics)
	}
	if strings.Contains(metrics, "beat_agent_enrollment_claim_seconds") {
		t.Fatalf("negative latency produced a sample:\n%s", metrics)
	}
}

func TestRegistryEnrollmentStatisticsSnapshot(t *testing.T) {
	registry := NewRegistry()
	registry.SetEnrollmentStatistics(func() (EnrollmentStatistics, error) {
		return EnrollmentStatistics{
			Pending: 3, Approved: 1, Rejected: 2, Expired: 4, Claimed: 5,
			OldestPendingAge: 2 * time.Minute,
			ActiveGrants:     6, ExpiredGrants: 7, DisabledGrants: 8, RevokedGrants: 9,
		}, nil
	})
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_agent_enrollment_requests{status="pending"} 3`,
		`beat_agent_enrollment_requests{status="approved"} 1`,
		`beat_agent_enrollment_requests{status="rejected"} 2`,
		`beat_agent_enrollment_requests{status="expired"} 4`,
		`beat_agent_enrollment_requests{status="claimed"} 5`,
		"beat_agent_enrollment_pending_age_seconds 120",
		`beat_agent_enrollment_grants{state="active"} 6`,
		`beat_agent_enrollment_grants{state="expired"} 7`,
		`beat_agent_enrollment_grants{state="disabled"} 8`,
		`beat_agent_enrollment_grants{state="revoked"} 9`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}

	registry.SetEnrollmentStatistics(func() (EnrollmentStatistics, error) {
		return EnrollmentStatistics{}, errors.New("sqlite is unavailable")
	})
	failed := registry.Prometheus()
	if !strings.Contains(failed, `beat_agent_enrollment_requests{status="pending"} 3`) {
		t.Fatalf("failed scrape dropped the last snapshot:\n%s", failed)
	}
}

func TestRegistryEnrollmentMetricsWithoutSources(t *testing.T) {
	metrics := NewRegistry().Prometheus()
	for _, expected := range []string{
		"beat_agent_enrollment_challenges_total", "beat_agent_enrollment_requests{status=\"pending\"} 0",
		"beat_agent_enrollment_pending_age_seconds 0", "beat_agent_enrollment_grants{state=\"active\"} 0",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("empty registry missing %q:\n%s", expected, metrics)
		}
	}
	if strings.Contains(metrics, "beat_agent_enrollment_approval_seconds") {
		t.Fatalf("unobserved latency emitted a histogram:\n%s", metrics)
	}
}
