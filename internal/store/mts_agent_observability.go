package store

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/model"
)

const agentIngestOutcomeCount = 10

var agentIngestOutcomes = [agentIngestOutcomeCount]string{"accepted", "duplicate", "contact_only", "rejected", "capacity", "out_of_order", "conflict", "clock_skew", "expired", "storage_unavailable"}

type agentIngestCounters struct {
	outcomes          [agentIngestOutcomeCount]atomic.Uint64
	commits           atomic.Uint64
	commitNanoseconds atomic.Uint64
	retryAgeSeconds   atomic.Uint64
}

func (s *MTSStore) ObserveAgentReport(ack model.AgentReportACK) {
	outcome := agentReportOutcome(ack)
	for index, name := range agentIngestOutcomes {
		if outcome == name {
			s.agentStats.outcomes[index].Add(1)
			break
		}
	}
	if ack.Retryable && !ack.CapturedAt.IsZero() {
		age := uint64(min(max(time.Since(ack.CapturedAt), 0), model.MaximumAgentSampleAge) / time.Second)
		for previous := s.agentStats.retryAgeSeconds.Load(); age > previous; previous = s.agentStats.retryAgeSeconds.Load() {
			if s.agentStats.retryAgeSeconds.CompareAndSwap(previous, age) {
				break
			}
		}
	}
}

func agentReportOutcome(ack model.AgentReportACK) string {
	if ack.MetricsAccepted {
		if ack.Duplicate {
			return "duplicate"
		}
		return "accepted"
	}
	if ack.ContactAccepted {
		return "contact_only"
	}
	switch ack.ErrorCode {
	case "ingestion_capacity":
		return "capacity"
	case "sample_out_of_order":
		return "out_of_order"
	case "sample_conflict":
		return "conflict"
	case "clock_skew":
		return "clock_skew"
	case "sample_expired":
		return "expired"
	case "storage_unavailable", "storage_recovery_required":
		return "storage_unavailable"
	default:
		return "rejected"
	}
}

func (s *MTSStore) observeAgentCommit(started time.Time) {
	s.agentStats.commits.Add(1)
	s.agentStats.commitNanoseconds.Add(uint64(max(time.Since(started), 0)))
}

func (s *MTSStore) AgentIngestPrometheus() string {
	var output strings.Builder
	_, _ = output.WriteString("# TYPE beat_agent_reports_total counter\n")
	for index, name := range agentIngestOutcomes {
		_, _ = fmt.Fprintf(&output, "beat_agent_reports_total{outcome=%q} %d\n", name, s.agentStats.outcomes[index].Load())
	}
	_, _ = fmt.Fprintf(&output, "# TYPE beat_agent_commit_seconds summary\nbeat_agent_commit_seconds_count %d\nbeat_agent_commit_seconds_sum %g\n", s.agentStats.commits.Load(), float64(s.agentStats.commitNanoseconds.Load())/float64(time.Second))
	_, _ = fmt.Fprintf(&output, "# TYPE beat_agent_retry_max_age_seconds gauge\nbeat_agent_retry_max_age_seconds %d\n", s.agentStats.retryAgeSeconds.Load())
	count, oldest := s.preparedAgentStats(time.Now())
	_, _ = fmt.Fprintf(&output, "# TYPE beat_agent_prepared_reports gauge\nbeat_agent_prepared_reports %d\n# TYPE beat_agent_prepared_oldest_age_seconds gauge\nbeat_agent_prepared_oldest_age_seconds %g\n", count, oldest.Seconds())
	poisoned := 0
	if s.engine.poisoned.Load() {
		poisoned = 1
	}
	_, _ = fmt.Fprintf(&output, "# TYPE beat_agent_ingestion_recovery_required gauge\nbeat_agent_ingestion_recovery_required %d\n", poisoned)
	return output.String()
}

func (s *MTSStore) preparedAgentStats(now time.Time) (int, time.Duration) {
	s.agentPreparedMu.Lock()
	defer s.agentPreparedMu.Unlock()
	oldest := time.Duration(0)
	for _, prepared := range s.agentPrepared {
		oldest = max(oldest, now.Sub(prepared.admittedAt))
	}
	return len(s.agentPrepared), oldest
}
