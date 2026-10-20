package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentinstall"
	"github.com/beat/backend/internal/agentoperations"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/model"
)

func newUpdates(config agent.Config, path string) (*agentupdate.Controller, error) {
	if config.Updates == nil || !config.Updates.Enabled {
		return nil, nil
	}
	client, err := agentupdate.NewControlClient(agentupdate.HTTPConfig{ServerURL: config.ServerURL, AgentToken: config.AgentToken})
	if err != nil {
		return nil, err
	}
	installer := agentinstall.NewInstaller()
	return &agentupdate.Controller{RunningVersion: buildinfo.Version, Client: client, Build: func(ctx context.Context) (*agentupdate.Updater, error) { return installer.BuildUpdater(ctx, path) },
		OnError: func(err error) { slog.Warn("agent update unavailable", "code", agentupdate.ErrorCode(err)) }}, nil
}

func observeUpdateReports(reporter *agent.HTTPReporter, updates *agentupdate.Controller) {
	if updates == nil {
		return
	}
	reporter.WithReportObserver(updates.ReportAccepted, func(err error) {
		var reportErr *agent.ReportError
		protocolFailure := errors.As(err, &reportErr) && (reportErr.Code == "invalid_ack" || reportErr.Code == "invalid_report")
		updates.ReportFailed(protocolFailure)
	})
}

func runUpdateHelper(ctx context.Context, path string) error {
	installer := agentinstall.NewInstaller()
	supervisor := agentupdate.Supervisor{Build: func(ctx context.Context) (*agentupdate.Updater, error) { return installer.BuildUpdateHelper(ctx, path) },
		OnError: func(err error) { slog.Warn("agent update watchdog held", "code", agentupdate.ErrorCode(err)) }}
	return supervisor.Run(ctx)
}

func observeOperationFailure(updates *agentupdate.Controller, event agentoperations.Event) {
	if updates == nil {
		return
	}
	switch event.State {
	case "failed", "timed_out", "output_limit", "interrupted":
		updates.TaskFailed(event.AttemptID)
	}
}

type updateTaskObserver struct {
	inner   agent.TaskExecutor
	updates *agentupdate.Controller
}

func observedUpdateTasks(inner agent.TaskExecutor, updates *agentupdate.Controller) agent.TaskExecutor {
	if updates == nil {
		return inner
	}
	return updateTaskObserver{inner: inner, updates: updates}
}

func (observer updateTaskObserver) ExecuteTask(ctx context.Context, task model.AgentTask) model.AgentTaskResult {
	result := observer.inner.ExecuteTask(ctx, task)
	if !result.Success {
		observer.updates.TaskFailed(task.ID)
	}
	return result
}

func observeMonitoringError(updates *agentupdate.Controller, err error) {
	if updates == nil || errors.Is(err, context.Canceled) {
		return
	}
	var reportErr *agent.ReportError
	if errors.As(err, &reportErr) {
		return
	}
	updates.ReportFailed(false)
}
