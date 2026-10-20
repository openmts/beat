package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

type erasureReadinessStub struct{ err error }

func (stub erasureReadinessStub) Ready(context.Context) error { return stub.err }

type erasureStatusStub struct {
	status store.MetricErasureStatus
	err    error
}

func (stub erasureStatusStub) Status(context.Context) (store.MetricErasureStatus, error) {
	return stub.status, stub.err
}

func TestMetricErasureReadinessReportsDisabledAndDegraded(t *testing.T) {
	operations := &operability{}
	if check := operations.metricErasureCheck(t.Context()); check.Status != "ok" || check.Detail != "disabled" {
		t.Fatalf("disabled check = %+v", check)
	}
	operations.metricErasure = erasureReadinessStub{}
	if check := operations.metricErasureCheck(t.Context()); check.Status != "ok" || check.Detail != "" {
		t.Fatalf("healthy check = %+v", check)
	}
	// 耗尽重试的失败任务只降级细节，不让整个实例不可用。
	operations.metricErasure = erasureReadinessStub{err: errors.New("failed jobs require retry")}
	check := operations.metricErasureCheck(t.Context())
	if check.Status != "ok" || check.Detail != "degraded: failed jobs require retry" {
		t.Fatalf("degraded check = %+v", check)
	}
}

func TestMetricErasureMetricsSkipUnavailableSources(t *testing.T) {
	operations := &operability{}
	var output strings.Builder
	operations.writeMetricErasureMetrics(&output, t.Context())
	if output.Len() != 0 {
		t.Fatalf("disabled metrics wrote %q", output.String())
	}
	operations.metricErasureJobs = erasureStatusStub{err: errors.New("database is unavailable")}
	operations.writeMetricErasureMetrics(&output, t.Context())
	if output.Len() != 0 {
		t.Fatalf("failing metrics wrote %q", output.String())
	}
	operations.metricErasureJobs = erasureStatusStub{}
	operations.writeMetricErasureMetrics(&output, t.Context())
	text := output.String()
	if !strings.Contains(text, "beat_metric_erasure_jobs{state=\"queued\"} 0") {
		t.Fatalf("state series missing: %q", text)
	}
	if !strings.Contains(text, "# TYPE beat_metric_erasure_active_jobs gauge") ||
		!strings.Contains(text, "beat_metric_erasure_active_jobs{scope=") {
		t.Fatalf("scope series missing: %q", text)
	}
}
