package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestNewCommand(t *testing.T) {
	var configPath string
	command := newCommand(func(_ context.Context, path string) error {
		configPath = path
		return nil
	})
	if command.Name != "beat-agent" || len(command.Flags) != 1 {
		t.Fatalf("command = %#v", command)
	}
	if err := command.Run(context.Background(), []string{"beat-agent", "--config", "/tmp/agent.json"}); err != nil {
		t.Fatalf("run command: %v", err)
	}
	if configPath != "/tmp/agent.json" {
		t.Fatalf("config path = %q", configPath)
	}
}

func TestNewCommandErrors(t *testing.T) {
	command := newCommand(func(context.Context, string) error { return errors.New("run failed") })
	if err := command.Run(context.Background(), []string{"beat-agent", "--config", "config.json"}); err == nil {
		t.Fatal("expected action error")
	}
	command = newCommand(func(context.Context, string) error { return nil })
	if err := command.Run(context.Background(), []string{"beat-agent"}); err == nil {
		t.Fatal("expected required config error")
	}
	if err := command.Run(context.Background(), []string{"beat-agent", "--unknown"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	if err := command.Run(context.Background(), []string{"beat-agent", "--config", "config.json", "extra"}); err == nil {
		t.Fatal("expected positional argument error")
	}
}

func TestRunWithCanceledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	content := `{
  "server_url": "http://127.0.0.1:1",
  "agent_token": "token",
  "node_name": "node",
  "advertised_host": "127.0.0.1",
  "ssh_port": 22,
  "report_interval": "1s"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, path); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	if err := run(context.Background(), filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected config error")
	}
}

func TestMainStopsOnSignal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	content := `{
  "server_url": "http://127.0.0.1:1",
  "agent_token": "token",
  "node_name": "node",
  "advertised_host": "127.0.0.1",
  "ssh_port": 22,
  "report_interval": "1s"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldArgs := os.Args
	os.Args = []string{"beat-agent", "--config", path}
	t.Cleanup(func() { os.Args = oldArgs })
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	main()
}
