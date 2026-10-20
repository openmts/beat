package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerter"
	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestCommercialAlertWorkersDoNotRunLegacyRulesAndKeepCadenceAwareContacts(t *testing.T) {
	f := newCommercialFixture(t)
	if err := f.services.initializeCommercial(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	slow, fast := commercialNode(t, f, "slow"), commercialNode(t, f, "fast")
	commercialReceipt(t, f, slow)
	rules := &countedLegacyRules{}
	alerts := alerter.New(alerter.Config{Rules: rules, Events: f.stores.alertEvents, Metrics: f.stores.mts, Nodes: f.stores.nodes})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime := newRuntimeState()
	tasks := &backgroundTasks{cancel: cancel, alerts: alerts, runtime: runtime}
	startAlertWorkers(backgroundConfig{ctx: ctx, alerts: alerts, services: f.services, runtime: runtime}, tasks)
	defer tasks.stop()
	awaitCommercial(t, func() bool {
		node, err := f.stores.nodes.GetNode(t.Context(), fast.ID)
		return err == nil && node.Status == model.NodeStatusOffline && runtime.evaluation.Load() != nil
	})
	after, err := f.stores.nodes.GetNode(t.Context(), slow.ID)
	if err != nil || after.Status != model.NodeStatusOnline || rules.calls.Load() != 0 {
		t.Fatal(after, err, rules.calls.Load())
	}
	tasks.stop()
	tasks.stop()
	for _, worker := range runtime.workers.snapshot() {
		if worker.Running {
			t.Fatal("worker survived shutdown", worker)
		}
	}
}

func TestCommercialWorkerFallbackAndOperationMaintenanceStop(t *testing.T) {
	f := newCommercialFixture(t)
	if err := f.services.initializeCommercial(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	f.services.evaluation = nil
	rules := &countedLegacyRules{}
	alerts := alerter.New(alerter.Config{Rules: rules, Events: f.stores.alertEvents, Metrics: f.stores.mts, Nodes: f.stores.nodes})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime := newRuntimeState()
	tasks := &backgroundTasks{cancel: cancel, alerts: alerts, runtime: runtime}
	config := backgroundConfig{ctx: ctx, alerts: alerts, services: f.services, runtime: runtime}
	startAlertWorkers(config, tasks)
	startOperationWorker(config, tasks)
	defer tasks.stop()
	awaitCommercial(t, func() bool {
		workers := runtime.workers.snapshot()
		return rules.calls.Load() == 1 && len(workers) == 2 && !workers[1].LastSuccess.IsZero()
	})
	tasks.stop()
	startOperationWorker(backgroundConfig{services: &serverServices{}}, &backgroundTasks{})
}

func TestCommercialPeriodDeliversFailuresAndStopsImmediatelyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	failure := errors.New("fixture failure")
	runCommercialPeriodic(ctx, commercialPeriod{interval: time.Millisecond, run: func(context.Context) error { calls++; return failure }, observe: func(err error) {
		if !errors.Is(err, failure) {
			t.Error(err)
		}
		if calls == 2 {
			cancel()
		}
	}})
	if calls != 2 {
		t.Fatal(calls)
	}
	runCommercialPeriodic(ctx, commercialPeriod{interval: time.Millisecond, run: func(context.Context) error { t.Error("cancelled work ran"); return nil }, observe: func(error) {}})
}

func TestEvaluationRuntimeMetricsUseFixedNamesAndExposePendingLag(t *testing.T) {
	runtime := newRuntimeState()
	var output strings.Builder
	runtime.writeEvaluationMetrics(&output)
	if output.Len() != 0 {
		t.Fatal("invented an evaluation sample")
	}
	runtime.recordEvaluation(alerteval.RoundResult{Admitted: 10, Completed: 7, Failed: 1, Deferred: 3, BacklogComplete: true, OldestDueAge: 90 * time.Second, Degraded: true})
	runtime.writeEvaluationMetrics(&output)
	for _, line := range []string{"beat_alert_evaluation_round_failed 1", "beat_alert_evaluation_deferred 3", "beat_alert_evaluation_oldest_due_age_seconds 90", "beat_alert_evaluation_degraded 1"} {
		if !strings.Contains(output.String(), line) {
			t.Fatal(output.String())
		}
	}
	if strings.Contains(output.String(), "node_id") || strings.Contains(output.String(), "rule_id") {
		t.Fatal("unbounded labels")
	}
}

func TestCommercialWorkerDoesNotReportFailedEvaluationCommitAsSuccess(t *testing.T) {
	f := newCommercialFixture(t)
	if err := f.services.initializeCommercial(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	if _, err := f.stores.nodes.UpsertNode(t.Context(), "active", "192.0.2.5", 22); err != nil {
		t.Fatal(err)
	}
	if _, err := f.stores.alertRules.CreateAlertRule(t.Context(), &model.AlertRule{Name: "CPU", Metric: "cpu", Operator: "gt", Threshold: 80, Enabled: true, Severity: model.AlertSeverityWarning}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.stores.sqlite.DB.Exec(`CREATE TRIGGER reject_evaluation_update BEFORE UPDATE ON alert_evaluation_states BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	alerts := alerter.New(alerter.Config{Rules: &countedLegacyRules{}, Events: f.stores.alertEvents, Metrics: f.stores.mts, Nodes: f.stores.nodes})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime := newRuntimeState()
	tasks := &backgroundTasks{cancel: cancel, alerts: alerts, runtime: runtime}
	startAlertWorkers(backgroundConfig{ctx: ctx, alerts: alerts, services: f.services, runtime: runtime}, tasks)
	defer tasks.stop()
	awaitCommercial(t, func() bool {
		for _, worker := range runtime.workers.snapshot() {
			if worker.Name == "alerts" {
				return worker.ConsecutiveFails > 0
			}
		}
		return false
	})
	snapshot := runtime.evaluation.Load()
	if snapshot == nil || snapshot.result.Failed != 1 {
		t.Fatal(snapshot)
	}
	for _, worker := range runtime.workers.snapshot() {
		if worker.Name == "alerts" && !worker.LastSuccess.IsZero() {
			t.Fatal("failed commit recorded success", worker)
		}
	}
}
