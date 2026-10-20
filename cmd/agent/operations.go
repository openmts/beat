package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentoperations"
	"github.com/beat/backend/internal/agentupdate"
)

func newOperations(config agent.Config, path string) (*agentoperations.Runner, error) {
	return newOperationsWithUpdates(config, path, nil)
}

func newOperationsWithUpdates(config agent.Config, path string, updates *agentupdate.Controller) (*agentoperations.Runner, error) {
	if config.Operations == nil || !config.Operations.Enabled {
		return nil, nil
	}
	client, err := agentoperations.NewClient(agentoperations.ClientConfig{ServerURL: config.ServerURL, Token: config.AgentToken})
	if err != nil {
		return nil, err
	}
	return agentoperations.NewRunner(agentoperations.RunnerConfig{Local: *config.Operations, ConfigPath: path, Gateway: client, OnError: func(err error) { slog.Warn("agent operations unavailable", "code", agentoperations.ErrorCode(err)) }, OnEvent: func(event agentoperations.Event) { logOperationEvent(event); observeOperationFailure(updates, event) }})
}

func runAgentServices(ctx context.Context, config agent.Config, path string) (resultErr error) {
	updates, err := newUpdates(config, path)
	if err != nil {
		return err
	}
	components, err := monitoringComponentsWithUpdates(config, updates)
	if err != nil {
		if updates != nil {
			updates.Client.Close()
		}
		return err
	}
	if updates != nil {
		components = append(components, updates.Run)
	}
	operations, err := newOperationsWithUpdates(config, path, updates)
	if err != nil {
		if updates != nil {
			updates.Client.Close()
		}
		return err
	}
	if operations != nil {
		defer func() { resultErr = errors.Join(resultErr, operations.Close()) }()
		components = append(components, operations.Run)
	}
	diagnostics, err := newDiagnostics(config)
	if err != nil {
		if updates != nil {
			updates.Client.Close()
		}
		return err
	}
	if diagnostics != nil {
		defer func() { resultErr = errors.Join(resultErr, diagnostics.Close()) }()
		components = append(components, diagnostics.Run)
	}
	return runRuntimeComponents(ctx, components)
}

func runRuntimeComponents(ctx context.Context, components []func(context.Context) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	outcomes := make(chan error, len(components))
	for _, component := range components {
		go func() { outcomes <- component(runCtx) }()
	}
	var result error
	for range components {
		result = errors.Join(result, <-outcomes)
		cancel()
	}
	return result
}

func logOperationEvent(event agentoperations.Event) {
	slog.Info("agent operation transition", "attempt_id", event.AttemptID, "state", event.State, "reason", event.Reason, "duration_ms", event.DurationMS, "output_bytes", event.OutputBytes, "transferred_bytes", event.TransferredBytes)
}
