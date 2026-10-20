package agent

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	minimumReportBackoff   = time.Second
	maximumReportBackoff   = time.Minute
	maximumBackoffExponent = 6
)

// ReportError 只保留固定分类及 HTTP 状态，不保留 URL、凭据或响应正文。
type ReportError struct {
	Code       string
	Status     int
	Retryable  bool
	RetryAfter time.Duration
}

func (err *ReportError) Error() string {
	return fmt.Sprintf("agent report %s (status %d)", err.Code, err.Status)
}

func reportRetryDelay(attempt int, jitter float64, retryAfter time.Duration) time.Duration {
	base := min(minimumReportBackoff*time.Duration(1<<min(max(attempt, 0), maximumBackoffExponent)), maximumReportBackoff)
	delay := base/2 + time.Duration(float64(base/2)*min(max(jitter, 0), 1))
	return max(delay, min(max(retryAfter, 0), model.MaximumAgentSampleAge))
}

func parseReportRetryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err == nil {
		seconds = min(max(seconds, 0), int64(model.MaximumAgentSampleAge/time.Second))
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return min(max(at.Sub(now), 0), model.MaximumAgentSampleAge)
}

func safeReportCode(code string) string {
	switch code {
	case "unsupported_protocol", "clock_skew", "sample_expired", "invalid_report", "sample_conflict", "sample_out_of_order",
		"credential_rejected", "ingestion_capacity", "storage_unavailable", "storage_recovery_required", "custom_source_changed", "accelerator_policy_changed", "accelerator_capacity":
		return code
	default:
		return "rejected"
	}
}
