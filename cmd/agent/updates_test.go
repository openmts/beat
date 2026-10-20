package main

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/acceleratorcollect"
	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentinstall"
	"github.com/beat/backend/internal/agentoperations"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/model"
)

func TestUpdateInstallationCommands(t *testing.T) {
	actions := testActions()
	var received agentinstall.UpdateOptions
	actions.enableUpdates = func(_ context.Context, options agentinstall.UpdateOptions) error { received = options; return nil }
	args := []string{"beat-agent", "service", "enable-updates", "--config", "/private/agent.json", "--manifest", "/private/manifest.json", "--trust", "/private/trust.json", "--artifact", "linux", "--node-id", "node-id"}
	if err := quietCommand(actions).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if received.NodeID != "node-id" || received.ArtifactID != "linux" || received.ManifestPath != "/private/manifest.json" {
		t.Fatal(received)
	}
	if err := quietCommand(actions).Run(t.Context(), append(args, "unexpected")); err == nil {
		t.Fatal("extra argument accepted")
	}
	actions.enableUpdates = nil
	if err := quietCommand(actions).Run(t.Context(), args); err == nil {
		t.Fatal("missing action accepted")
	}
	path := ""
	actions.helper = func(_ context.Context, value string) error { path = value; return nil }
	if err := quietCommand(actions).Run(t.Context(), []string{"beat-agent", "update-helper", "--config", "/private/agent.json"}); err != nil {
		t.Fatal(err)
	}
	if path != "/private/agent.json" {
		t.Fatal(path)
	}
	if err := quietCommand(actions).Run(t.Context(), []string{"beat-agent", "update-helper", "--config", "/private/agent.json", "extra"}); err == nil {
		t.Fatal("helper extra argument accepted")
	}
}

func TestUpdateRuntimeDefaultAndCanceledStartup(t *testing.T) {
	if updates, err := newUpdates(agent.Config{}, "unused"); err != nil || updates != nil {
		t.Fatal(updates, err)
	}
	config := agent.Config{Updates: &agentupdate.Settings{Enabled: true, NodeID: "node"}}
	if _, err := newUpdates(config, "unused"); err == nil {
		t.Fatal("invalid URL accepted")
	}
	config.ServerURL = "https://127.0.0.1:1"
	config.AgentToken = "private"
	config.NodeName = "fixture"
	config.AdvertisedHost = "127.0.0.1"
	config.SSHPort = 22
	config.ReportInterval = time.Second
	updates, err := newUpdates(config, "/missing-private-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = updates.Build(t.Context()); err == nil {
		t.Fatal("missing config accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = updates.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runUpdateHelper(ctx, "/missing-private-fixture"); err != nil {
		t.Fatal(err)
	}
	if err = runAgentServices(ctx, config, "/missing-private-fixture"); err != nil {
		t.Fatal(err)
	}
}

type updateTestExecutor struct{ success bool }

func (executor updateTestExecutor) ExecuteTask(_ context.Context, task model.AgentTask) model.AgentTaskResult {
	return model.AgentTaskResult{TaskID: task.ID, Success: executor.success}
}

func TestUpdateReportAndTaskObserversPreserveResults(t *testing.T) {
	updates := &agentupdate.Controller{}
	for _, success := range []bool{false, true} {
		executor := observedUpdateTasks(updateTestExecutor{success}, updates)
		result := executor.ExecuteTask(t.Context(), model.AgentTask{ID: "task"})
		if result.Success != success || result.TaskID != "task" {
			t.Fatal(result)
		}
	}
	if result := observedUpdateTasks(updateTestExecutor{true}, nil).ExecuteTask(t.Context(), model.AgentTask{}); !result.Success {
		t.Fatal(result)
	}
	observeOperationFailure(nil, agentoperations.Event{})
	for _, state := range []string{"failed", "succeeded", "timed_out", "output_limit", "interrupted"} {
		observeOperationFailure(updates, agentoperations.Event{AttemptID: state, State: state})
	}
	reporter, err := agent.NewHTTPReporter(agent.Config{ServerURL: "https://127.0.0.1:1", AgentToken: "private", NodeName: "fixture", AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	observeUpdateReports(reporter, nil)
	observeUpdateReports(reporter, updates)
	if _, err = reporter.Report(t.Context(), agent.ReportSample{}); err == nil {
		t.Fatal("invalid sample accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = reporter.Report(ctx, agent.ReportSample{SampleID: "d6d7a562-05aa-440b-a9e0-3bddfba76dce", CapturedAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("canceled report accepted")
	}
}

func TestUpdateStartupFailureClosesIdleClients(t *testing.T) {
	config := agent.Config{Updates: &agentupdate.Settings{Enabled: true, NodeID: "node"}}
	if err := runAgentServices(t.Context(), config, "/missing-private-fixture"); err == nil {
		t.Fatal("invalid update config accepted")
	}
	config.ServerURL = "https://127.0.0.1:1"
	config.AgentToken = "private"
	config.NodeName = "fixture"
	config.AdvertisedHost = "127.0.0.1"
	config.SSHPort = 22
	config.ReportInterval = time.Second
	config.Accelerators = &acceleratorcollect.Config{MaximumDevices: -1}
	if err := runAgentServices(t.Context(), config, "/missing-private-fixture"); err == nil {
		t.Fatal("invalid monitoring config accepted")
	}
	config.Accelerators = nil
	config.Operations = &agentoperations.Config{Enabled: true, StateDir: "relative"}
	if err := runAgentServices(t.Context(), config, "/missing-private-fixture"); err == nil {
		t.Fatal("invalid operation config accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := runUpdateHelper(ctx, "/missing-private-fixture"); err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringFailureCounterSkipsAlreadyCountedReportErrors(t *testing.T) {
	updates := &agentupdate.Controller{}
	observeMonitoringError(nil, context.DeadlineExceeded)
	observeMonitoringError(updates, context.Canceled)
	observeMonitoringError(updates, &agent.ReportError{Code: "invalid_ack"})
	observeMonitoringError(updates, context.DeadlineExceeded)
}
