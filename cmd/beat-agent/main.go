package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"beat/internal/agent"
	"beat/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		if err := agent.Install(); err != nil {
			fmt.Fprintln(os.Stderr, "Agent installation failed:", err)
			os.Exit(1)
		}
		fmt.Println("Agent installed: /usr/local/bin/beat-agent")
		fmt.Println("Service: /etc/systemd/system/beat-agent.service")
		fmt.Println("Configuration: /etc/beat/agent.yaml")
		return
	}
	doctorMode := len(os.Args) > 1 && os.Args[1] == "doctor"
	args := os.Args[1:]
	if doctorMode {
		args = args[1:]
	}
	opts, err := agent.ParseOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage()
			return
		}
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(2)
	}
	if opts.ShowVersion {
		fmt.Printf("beat-agent %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		return
	}
	if doctorMode {
		report := agent.Diagnose(opts.ServerURL, opts.Token, opts.StateFile, opts.DiskPath)
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		if !report.Healthy {
			os.Exit(1)
		}
		return
	}
	if opts.Interval < 5*time.Second {
		fmt.Fprintln(os.Stderr, "interval must be at least 5s")
		os.Exit(2)
	}
	if strings.TrimSpace(opts.Token) == "" {
		fmt.Fprintln(os.Stderr, "an enrollment token is required; configure token in agent.yaml or pass --token")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	agentID, err := loadAgentID(opts.StateFile)
	if err != nil {
		logger.Error("agent_identity_failed", "error", err)
		os.Exit(1)
	}
	runtime, err := agent.NewRuntime(agent.RuntimeConfig{
		ServerURL: opts.ServerURL, Token: opts.Token, AgentID: agentID, StateFile: opts.StateFile,
		NodeName: opts.Name, Labels: parseLabels(opts.LabelsRaw), Interval: opts.Interval,
		DiskPath: opts.DiskPath, Logger: logger, InsecureSkipVerify: opts.InsecureSkipVerify,
	})
	if err != nil {
		logger.Error("agent_runtime_init_failed", "error", err)
		os.Exit(1)
	}
	if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
		if errors.Is(err, agent.ErrUpgradeRestart) {
			logger.Info("agent_restarting_after_upgrade")
			os.Exit(75)
		}
		logger.Error("agent_runtime_failed", "error", err)
		os.Exit(1)
	}
}

func loadAgentID(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("state file path must not be empty")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to use symlink as state file: %s", path)
		}
		if info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(path, 0o600); err != nil {
				return "", fmt.Errorf("tighten state file permissions: %w", err)
			}
		}
	}
	if body, err := os.ReadFile(path); err == nil {
		if value := strings.TrimSpace(string(body)); value != "" {
			return value, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := "agent-" + hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func parseLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(key) != "" {
			labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return labels
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  beat-agent [flags]          Start the Agent
  beat-agent doctor [flags]   Validate configuration and connectivity

Flags:
  --config PATH                YAML configuration file
  --server URL                 Beat server URL (default http://127.0.0.1:9099)
  --token TOKEN                short-lived enrollment token (required)
  --interval DURATION          collection interval (default 15s, minimum 5s)
  --name NAME                  node display name (default hostname)
  --labels KEY=VALUE,...       node labels (default env=production)
  --state-file PATH            persistent Agent identity file
  --disk-path PATH             filesystem used for disk metrics (default /)
  --insecure-skip-verify       skip HTTPS certificate verification
  --version                    print version`)
	fmt.Fprintln(os.Stderr, `
Administration:
  beat-agent install           install the Agent and systemd unit (root required)`)
}
