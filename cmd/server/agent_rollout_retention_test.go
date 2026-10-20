package main

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
)

func TestAgentRetentionPolicyDefaultsToKeepEverything(t *testing.T) {
	policy, err := readAgentRetentionPolicy(func(string) string { return "" })
	if err != nil || policy.Enabled() || policy != (agentrollout.RetentionPolicy{}) {
		t.Fatal(policy, err)
	}
}

func TestAgentRetentionPolicyParsesAndRejectsOutsideBounds(t *testing.T) {
	values := map[string]string{agentRolloutRetentionEnv: "30", agentReleaseRetentionEnv: "7"}
	policy, err := readAgentRetentionPolicy(func(name string) string { return values[name] })
	if err != nil || policy.RolloutDays != 30 || policy.ReleaseDays != 7 {
		t.Fatal(policy, err)
	}
	for name, value := range map[string]string{
		"malformed": "thirty",
		"negative":  "-1",
		"oversized": "4000",
	} {
		if _, err := readAgentRetentionPolicy(func(string) string { return value }); err == nil {
			t.Fatal("invalid retention accepted", name, value)
		}
	}
}

func TestAgentRolloutRetentionWorkerRunsOnlyWhenConfigured(t *testing.T) {
	for name, test := range map[string]struct {
		days string
		want string
	}{
		"without policy": {"", "agent_rollouts"},
		"with policy":    {"30", "agent_rollouts,agent_rollout_retention"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCommercialFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := newRuntimeState()
			tasks := &backgroundTasks{}
			config := backgroundConfig{ctx: ctx, services: f.services, runtime: state}
			path, enabled := rolloutStartupFixture(t, f.directory)
			writeRolloutStartup(t, path, enabled)
			t.Setenv(agentRolloutRetentionEnv, test.days)
			if err := f.services.initializeAgentRollouts(ctx, f.stores, f.directory); err != nil {
				t.Fatal(err)
			}
			startAgentRolloutWorker(config, tasks)
			if names := scheduledWorkerNames(state); names != test.want {
				t.Fatal("unexpected worker set", names)
			}
			cancel()
			tasks.wait.Wait()
			for _, worker := range state.schedulerWorkers() {
				if worker.Running || worker.Failure != "" || !knownWorker(worker.Name) {
					t.Fatal("retention worker did not stop cleanly", worker)
				}
			}
		})
	}
}

func scheduledWorkerNames(state *runtimeState) (names string) {
	for index, worker := range state.schedulerWorkers() {
		if index > 0 {
			names += ","
		}
		names += worker.Name
	}
	return names
}
