package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
)

func TestSnapshotClosedMTSIsCountedWithoutPublicPayload(t *testing.T) {
	fixture := newCommercialFixture(t)
	router := api.NewRouter(fixture.stores.nodes, fixture.stores.groups, fixture.stores.sshKeys, fixture.stores.alertRules, fixture.stores.alertChannels, fixture.stores.alertEvents, fixture.stores.mts)
	state := newRuntimeState()
	builder := state.publicBuilder(router)
	if err := fixture.stores.mts.Close(); err != nil {
		t.Fatal(err)
	}
	value, err := builder(t.Context())
	if !errors.Is(err, handler.ErrPublicMetricQuery) || value.Value != nil || state.mtsSnapshotFailures.Load() != 1 {
		t.Fatal(value, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = builder(canceled)
	if err == nil || state.mtsSnapshotFailures.Load() != 1 {
		t.Fatal("cancellation counted as query failure", err)
	}
	var output strings.Builder
	state.writeWorkerMetrics(&output)
	if !strings.Contains(output.String(), `beat_mts_query_failures_total{consumer="metrics_fanout",class="read_failed"} 1`) {
		t.Fatal(output.String())
	}
}

func TestWorkerMetricsRejectUnknownLabelsAndPreserveFailureCounts(t *testing.T) {
	state := newRuntimeStateWithWorkers("alerts", "arbitrary-secret-label")
	worker := state.workers.workers[0]
	worker.observe()(errors.New("secret failure"))
	worker.observe()(nil)
	worker.failed.Store(true)
	worker.panicked.Store(true)
	var output strings.Builder
	state.writeWorkerMetrics(&output)
	if strings.Contains(output.String(), "secret") || !strings.Contains(output.String(), `beat_worker_failures_total{worker="alerts",class="iteration_failed"} 1`) || !strings.Contains(output.String(), `beat_worker_unexpected_exits_total{worker="alerts",reason="panic"} 1`) {
		t.Fatal(output.String())
	}
}

func TestWorkerInitialGraceAndCancellationAreExplicit(t *testing.T) {
	now := time.Now()
	worker := newSupervisedWorker("alerts", time.Second)
	worker.running.Store(true)
	worker.started.Store(true)
	worker.startedAt.Store(now.UnixNano())
	if ok, _ := worker.healthy(now); !ok {
		t.Fatal("initial grace unavailable")
	}
	if ok, reason := worker.healthy(now.Add(startupGrace + time.Second)); ok || reason != "starting" {
		t.Fatal(ok, reason)
	}
	worker.observe()(context.Canceled)
	if worker.consecutiveFails.Load() != 0 || !atomicTime(&worker.lastFailure).IsZero() {
		t.Fatal("cancellation recorded as failure")
	}
}

func TestWorkerLagUsesAttemptProgressDespiteIterationFailures(t *testing.T) {
	worker := newSupervisedWorker("alerts", time.Minute)
	worker.running.Store(true)
	worker.lastSuccess.Store(time.Now().Add(-time.Hour).UnixNano())
	worker.observe()(errors.New("storage temporarily unavailable"))
	if value := worker.snapshot(); value.SchedulerLagSeconds != 0 || value.ConsecutiveFails != 1 {
		t.Fatal("failure age was misreported as scheduler lag", value)
	}
	worker.lastAttempt.Store(0)
	worker.startedAt.Store(time.Now().Add(-3 * time.Minute).UnixNano())
	if value := worker.snapshot(); value.SchedulerLagSeconds < 120 {
		t.Fatal("stalled initial iteration was not observed", value)
	}
}
