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

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/buildinfo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := newCommand(run).Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newCommand(runAgent func(context.Context, string) error) *cli.Command {
	return &cli.Command{
		Name:    "beat-agent",
		Usage:   "collect and report Beat node metrics",
		Version: buildinfo.Version,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: "path to the agent JSON config", Required: true},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return fmt.Errorf("positional arguments are not supported")
			}
			return runAgent(ctx, command.String("config"))
		},
	}
}

func run(ctx context.Context, configPath string) error {
	agent.AgentVersion = buildinfo.Version
	config, err := agent.LoadConfig(configPath)
	if err != nil {
		return err
	}
	reporter, err := agent.NewHTTPReporter(config)
	if err != nil {
		return err
	}
	collector := agent.NewCollector(agent.SystemSampler{}, agentNow)
	runner := agent.NewRunner(agent.RunnerOptions{
		Interval: config.ReportInterval, Collector: collector, Reporter: reporter,
		OnError: func(runErr error) { slog.Error("agent report failed", "error", runErr) },
	})
	networkClient, err := agent.NewNetworkHTTPClient(config)
	if err != nil {
		return err
	}
	networkRunner := agent.NewNetworkRunner(agent.NetworkRunnerOptions{
		Client: networkClient, Prober: agent.NewNetworkProber(),
		OnError: func(runErr error) { slog.Error("network probe failed", "error", runErr) },
	})
	return runComponents(ctx, runner.Run, networkRunner.Run)
}

func runComponents(
	ctx context.Context,
	metrics func(context.Context) error,
	network func(context.Context) error,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- metrics(runCtx) }()
	go func() { errorsCh <- network(runCtx) }()
	first := <-errorsCh
	cancel()
	second := <-errorsCh
	return errors.Join(first, second)
}

var agentNow = func() time.Time { return time.Now().UTC() }
