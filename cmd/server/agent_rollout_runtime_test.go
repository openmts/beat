package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRolloutWorkerStartsOnlyWhenEnabledAndJoinsOnCancellation(t *testing.T) {
	f := newCommercialFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := newRuntimeState()
	tasks := &backgroundTasks{}
	config := backgroundConfig{ctx: ctx, services: f.services, runtime: state}
	startAgentRolloutWorker(config, tasks)
	if len(state.schedulerWorkers()) != 0 || rolloutMonitoring(f.services) != nil {
		t.Fatal("missing service started a worker")
	}
	if err := f.services.initializeAgentRollouts(ctx, f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	startAgentRolloutWorker(config, tasks)
	if len(state.schedulerWorkers()) != 0 {
		t.Fatal("disabled service started a worker")
	}
	path, enabled := rolloutStartupFixture(t, f.directory)
	writeRolloutStartup(t, path, enabled)
	if err := f.services.initializeAgentRollouts(ctx, f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	startAgentRolloutWorker(config, tasks)
	t.Cleanup(func() { cancel(); tasks.wait.Wait() })
	deadline := time.Now().Add(2 * time.Second)
	for {
		workers := state.schedulerWorkers()
		if len(workers) == 1 && !workers[0].LastSuccess.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery worker did not complete its initial pass")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	tasks.wait.Wait()
	worker := state.schedulerWorkers()[0]
	if worker.Name != "agent_rollouts" || worker.Running || worker.Failure != "" || !knownWorker(worker.Name) {
		t.Fatal("worker did not stop cleanly", worker)
	}
}

func TestRolloutReadinessAndMetricsDistinguishDisabled(t *testing.T) {
	if check := (&operability{}).agentRolloutCheck(t.Context()); check.Status != "ok" || check.Detail != "disabled" {
		t.Fatal(check)
	}
	f := newCommercialFixture(t)
	if err := f.services.initializeAgentRollouts(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	if check := (&operability{agentRollouts: rolloutMonitoring(f.services)}).agentRolloutCheck(t.Context()); check.Detail != "disabled" {
		t.Fatal(check)
	}
	for _, test := range []struct {
		check          readinessCheck
		enabled, ready string
	}{
		{readinessCheck{Status: "ok", Detail: "disabled"}, "0", "0"},
		{readinessCheck{Status: "ok"}, "1", "1"},
		{readinessCheck{Status: "error", Detail: "unavailable"}, "1", "0"},
	} {
		var output strings.Builder
		writeAgentRolloutMetrics(&output, test.check)
		if !strings.Contains(output.String(), "beat_agent_rollout_enabled "+test.enabled+"\n") || !strings.Contains(output.String(), "beat_agent_rollout_ready "+test.ready+"\n") {
			t.Fatal("incorrect rollout readiness metrics", output.String())
		}
	}
}
