package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeSecurityMetrics struct {
	snapshot adminauth.SecurityMetricsSnapshot
	rows     store.SecurityDiagnostics
	age      float64
	found    bool
	failed   bool
}

func (fake fakeSecurityMetrics) SecurityMetricsSnapshot() adminauth.SecurityMetricsSnapshot {
	return fake.snapshot
}

func (fake fakeSecurityMetrics) SecurityMetrics(context.Context) (store.SecurityDiagnostics, error) {
	if fake.failed {
		return store.SecurityDiagnostics{}, context.Canceled
	}
	return fake.rows, nil
}

func (fake fakeSecurityMetrics) SecurityOutboxAge(context.Context, time.Time) (float64, bool, error) {
	return fake.age, fake.found, nil
}

func TestSecurityMetricsExposeFixedLabelVocabulary(t *testing.T) {
	source := fakeSecurityMetrics{rows: store.SecurityDiagnostics{Thresholds: 4, Fingerprints: 9}, age: 12.5, found: true,
		snapshot: adminauth.SecurityMetricsSnapshot{
			Kinds:      map[string]uint64{model.SecurityEventLoginSucceeded: 3, model.SecurityEventLoginSuspicious: 1},
			Severities: map[string]uint64{model.SecuritySeverityHigh: 1},
			Reasons:    map[string]uint64{model.SecurityReasonRateLimitLockout: 1},
		}}
	var output strings.Builder
	operations := &operability{security: source}
	operations.writeSecurityMetrics(&output, context.Background())
	text := output.String()
	for _, series := range []string{
		`beat_security_login_events_total{kind="security.login_succeeded"} 3`,
		`beat_security_login_events_total{severity="high"} 1`,
		`beat_security_login_suspicious_total{reason="rate_limit_lockout"} 1`,
		"beat_security_transaction_failures_total 0",
		"beat_security_outbox_failures_total 0",
		"beat_security_threshold_rows 4",
		"beat_security_fingerprint_rows 9",
		"beat_security_oldest_pending_seconds 12.5",
	} {
		if !strings.Contains(text, series) {
			t.Fatalf("metrics output is missing %q:\n%s", series, text)
		}
	}
	if strings.Contains(text, "203.0.113") {
		t.Fatal("metrics leaked a source address")
	}
}

func TestSecurityMetricsSkipGaugesWhenPersistenceFails(t *testing.T) {
	var output strings.Builder
	operations := &operability{security: fakeSecurityMetrics{failed: true}}
	operations.writeSecurityMetrics(&output, context.Background())
	if strings.Contains(output.String(), "beat_security_threshold_rows") {
		t.Fatal("failed persistence still exported gauges")
	}
	empty := &operability{}
	empty.writeSecurityMetrics(&output, context.Background())
}
