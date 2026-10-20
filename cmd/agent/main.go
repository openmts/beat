package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/acceleratorcollect"
	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentinstall"
	"github.com/beat/backend/internal/agentoperations"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/buildinfo"
)

func main() {
	if handled, code := agentoperations.HandleHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := newCommand(run).Run(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newCommand(runAgent func(context.Context, string) error) *cli.Command {
	installer := agentinstall.NewInstaller()
	return newCommandWithActions(agentActions{run: runAgent, join: installer.Join, install: installer.Install,
		uninstall: installer.Uninstall, enableUpdates: installer.EnableUpdates, enroll: installer.Enroll,
		helper: runUpdateHelper})
}

func newCommandWithActions(actions agentActions) *cli.Command {
	return &cli.Command{
		Name:    "beat-agent",
		Usage:   "collect and report Beat node metrics",
		Version: buildinfo.Version,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: "path to the agent JSON config"},
		},
		Commands: []*cli.Command{agentEnrollCommand(actions), agentJoinCommand(actions),
			agentServiceCommand(actions), agentUpdateHelperCommand(actions)},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return fmt.Errorf("positional arguments are not supported")
			}
			if command.String("config") == "" {
				return errors.New("--config is required for foreground mode")
			}
			return actions.run(ctx, command.String("config"))
		},
	}
}

func run(ctx context.Context, configPath string) error {
	agent.AgentVersion = buildinfo.Version
	config, err := agent.LoadConfig(configPath)
	if err != nil {
		return err
	}
	return runAgentServices(ctx, config, configPath)
}

func monitoringComponents(config agent.Config) ([]func(context.Context) error, error) {
	return monitoringComponentsWithUpdates(config, nil)
}

func monitoringComponentsWithUpdates(config agent.Config, updates *agentupdate.Controller) ([]func(context.Context) error, error) {
	reporter, err := agent.NewHTTPReporter(config)
	if err != nil {
		return nil, err
	}
	observeUpdateReports(reporter, updates)
	gpuConfig := acceleratorcollect.Config{}
	if config.Accelerators != nil {
		gpuConfig = *config.Accelerators
	}
	gpu, err := acceleratorcollect.New(gpuConfig, nil)
	if err != nil {
		return nil, err
	}
	reporter.WithAcceleratorPolicyReceiver(gpu.ApplyPolicy)
	collector := agent.NewCollector(agent.SystemSampler{}, agentNow)
	registry := agent.NewCustomMetricRegistry()
	runner := agent.NewRunner(agent.RunnerOptions{
		Interval: config.ReportInterval, Collector: collector, Accelerators: gpu,
		CustomSampler: agent.NewCustomMetricCollector(registry),
		Reporter:      reporter,
		SelfHealer:    agent.NewAuthorizedKeySelfHealer(""),
		OnError: func(runErr error) {
			slog.Error("agent report failed", "error", runErr)
			observeMonitoringError(updates, runErr)
		},
	})
	networkClient, err := agent.NewNetworkHTTPClient(config)
	if err != nil {
		return nil, err
	}
	networkRunner := agent.NewNetworkRunner(agent.NetworkRunnerOptions{
		Client: networkClient, Prober: agent.NewNetworkProber(),
		OnError: func(runErr error) { slog.Error("network probe failed", "error", runErr) },
	})
	channel, err := agent.NewChannel(config.ServerURL, config.AgentToken,
		observedUpdateTasks(agent.NewMultiTaskExecutor(agent.NewAuthorizedKeyExecutor(""), registry), updates))
	if err != nil {
		return nil, err
	}
	channel.SetOnError(func(runErr error) { slog.Error("agent channel failed", "error", runErr) })
	return []func(context.Context) error{runner.Run, acceleratorComponent(gpu), networkRunner.Run, channel.Run}, nil
}

func runComponents(ctx context.Context, components ...func(context.Context) error) error {
	return runRuntimeComponents(ctx, components)
}

var agentNow = func() time.Time { return time.Now().UTC() }
