package main

import (
	"context"
	"strings"
	"testing"
)

// TestAgentRetentionHelpersWithoutServices 覆盖保留期辅助函数在缺服务时的安全缺省。
func TestAgentRetentionHelpersWithoutServices(t *testing.T) {
	if agentRetentionConfigured(t.Context(), nil) {
		t.Fatal("missing retention service reported a configured policy")
	}
	if err := pruneAgentRollouts(t.Context(), nil, nil); err != nil {
		t.Fatalf("prune without a retention service = %v", err)
	}
	var output strings.Builder
	writeAgentRolloutMetrics(&output, readinessCheck{Detail: "disabled"})
	writeAgentRolloutMetrics(&output, readinessCheck{Status: "ok"})
	if !strings.Contains(output.String(), "beat_agent_rollout_ready 1") ||
		!strings.Contains(output.String(), "beat_agent_rollout_enabled 1") {
		t.Fatalf("agent rollout metrics = %q", output.String())
	}
	var disabled strings.Builder
	writeAgentRolloutMetrics(&disabled, readinessCheck{Status: "ok", Detail: "disabled"})
	if !strings.Contains(disabled.String(), "beat_agent_rollout_enabled 0") {
		t.Fatalf("disabled agent rollout metrics = %q", disabled.String())
	}
	startAgentRolloutRetentionWorker(backgroundConfig{ctx: t.Context(), services: &serverServices{}},
		&backgroundTasks{}, nil)
}

// TestAgentRolloutRetentionPruneAndWorker 覆盖保留期服务的真实清理与后台注册。
func TestAgentRolloutRetentionPruneAndWorker(t *testing.T) {
	f := newCommercialFixture(t)
	configPath, config := rolloutStartupFixture(t, f.directory)
	writeRolloutStartup(t, configPath, config)
	t.Setenv("BEAT_AGENT_ROLLOUT_CONFIG", configPath)
	t.Setenv(agentRolloutRetentionEnv, "7")
	if err := f.services.initializeAgentRollouts(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	if !f.services.agentRetention.Enabled() {
		t.Fatal("retention policy stayed disabled")
	}
	if agentRetentionConfigured(t.Context(), f.services.agentRetentionAdmin) {
		t.Fatal("unconfigured retention settings reported enabled")
	}
	if err := pruneAgentRollouts(t.Context(), f.services.agentRetentionAdmin, f.services.agentRollouts); err != nil {
		t.Fatalf("retention prune = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime := newRuntimeState()
	tasks := &backgroundTasks{cancel: cancel, runtime: runtime}
	background := backgroundConfig{ctx: ctx, services: f.services, runtime: runtime}
	startAgentRolloutRetentionWorker(background, tasks, f.services.agentRollouts)
	defer tasks.stop()
	awaitCommercial(t, func() bool {
		workers := runtime.workers.snapshot()
		return len(workers) == 1 && !workers[0].LastSuccess.IsZero()
	})
	tasks.stop()
}
