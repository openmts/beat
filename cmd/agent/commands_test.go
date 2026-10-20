package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/agentinstall"
)

func testActions() agentActions {
	return agentActions{
		run: func(context.Context, string) error { return nil },
		join: func(context.Context, agentinstall.JoinOptions) (agentinstall.Summary, error) {
			return agentinstall.Summary{NodeID: "node-id", ServerURL: "https://beat.example.com", ConfigPath: agentinstall.DefaultConfigPath}, nil
		},
		install: func(context.Context, string) error { return nil },
		uninstall: func(context.Context, agentinstall.UninstallOptions) (agentinstall.UninstallSummary, error) {
			return agentinstall.UninstallSummary{}, nil
		},
	}
}

func quietCommand(actions agentActions) *cli.Command {
	command := newCommandWithActions(actions)
	command.Writer, command.ErrWriter = io.Discard, io.Discard
	return command
}

func TestJoinCommandPassesOptions(t *testing.T) {
	actions := testActions()
	var received agentinstall.JoinOptions
	actions.join = func(_ context.Context, options agentinstall.JoinOptions) (agentinstall.Summary, error) {
		received = options
		return agentinstall.Summary{NodeID: "node-id", ServerURL: options.ServerURL, ConfigPath: agentinstall.DefaultConfigPath}, nil
	}
	command := quietCommand(actions)
	var output bytes.Buffer
	command.Writer = &output
	args := []string{"beat-agent", "join", "--server", "http://beat.example.com", "--token", "secret-value",
		"--allow-http", "--name", "node-a", "--host", "192.0.2.1", "--ssh-port", "2222"}
	if err := command.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	want := agentinstall.JoinOptions{ServerURL: "http://beat.example.com", Token: "secret-value", AllowHTTP: true,
		Name: "node-a", Host: "192.0.2.1", SSHPort: 2222}
	if want != received {
		t.Fatalf("want %+v, got %+v", want, received)
	}
	if !strings.Contains(output.String(), "node-id") || strings.Contains(output.String(), "secret-value") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestJoinCommandReadsTokenFromStdin(t *testing.T) {
	actions := testActions()
	var token string
	actions.join = func(_ context.Context, options agentinstall.JoinOptions) (agentinstall.Summary, error) {
		token = options.Token
		return agentinstall.Summary{}, nil
	}
	command := quietCommand(actions)
	command.Reader = strings.NewReader("  private-token\n")
	if err := command.Run(context.Background(), []string{"beat-agent", "join", "--server", "https://beat.example.com", "--token-stdin"}); err != nil {
		t.Fatal(err)
	}
	if token != "private-token" {
		t.Fatalf("want trimmed token, got %q", token)
	}
}

func TestJoinCommandValidation(t *testing.T) {
	base := []string{"beat-agent", "join", "--server", "https://beat.example.com"}
	cases := [][]string{
		base, append(append([]string(nil), base...), "--token", "token", "--token-stdin"),
		append(append([]string(nil), base...), "--token", "token", "unexpected"),
		append(append([]string(nil), base...), "--token", "token", "--ssh-port", "0"),
		append(append([]string(nil), base...), "--token", "token", "--ssh-port", "65536"),
		{"beat-agent", "join", "--token", "token"},
		{"beat-agent", "--config", "existing.json", "join", "--server", "https://beat.example.com", "--token", "token"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := quietCommand(testActions()).Run(context.Background(), args); err == nil {
				t.Fatal("expected command validation error")
			}
		})
	}
}

func TestJoinStdinErrors(t *testing.T) {
	for name, reader := range map[string]io.Reader{"empty": strings.NewReader("\n"), "too long": strings.NewReader(strings.Repeat("a", maxJoinTokenInput+1)), "read": failedReader{}} {
		t.Run(name, func(t *testing.T) {
			command := quietCommand(testActions())
			command.Reader = reader
			if err := command.Run(context.Background(), []string{"beat-agent", "join", "--server", "https://beat.example.com", "--token-stdin"}); err == nil {
				t.Fatal("expected stdin error")
			}
		})
	}
}

func TestServiceInstallCommand(t *testing.T) {
	actions := testActions()
	var configPath string
	actions.install = func(_ context.Context, path string) error { configPath = path; return nil }
	for _, path := range []string{agentinstall.DefaultConfigPath, "/path with spaces/agent.json"} {
		args := []string{"beat-agent", "service", "install"}
		if path != agentinstall.DefaultConfigPath {
			args = append(args, "--config", path)
		}
		if err := quietCommand(actions).Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if path != configPath {
			t.Fatalf("want %q, got %q", path, configPath)
		}
	}
	if err := quietCommand(actions).Run(context.Background(), []string{"beat-agent", "service", "install", "extra"}); err == nil {
		t.Fatal("expected positional argument error")
	}
}

func TestServiceUninstallCommand(t *testing.T) {
	actions := testActions()
	var received agentinstall.UninstallOptions
	actions.uninstall = func(_ context.Context, options agentinstall.UninstallOptions) (agentinstall.UninstallSummary, error) {
		received = options
		return agentinstall.UninstallSummary{Removed: []string{"/etc/systemd/system/beat-agent.service"},
			Retained: []string{"/etc/beat-agent"}}, nil
	}
	if err := quietCommand(actions).Run(context.Background(), []string{"beat-agent", "service", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	if received.Purge {
		t.Fatal("purge must stay off unless requested")
	}
	command := quietCommand(actions)
	var output bytes.Buffer
	command.Writer = &output
	if err := command.Run(context.Background(), []string{"beat-agent", "service", "uninstall", "--purge"}); err != nil {
		t.Fatal(err)
	}
	if !received.Purge {
		t.Fatal("expected --purge to reach the installer")
	}
	for _, want := range []string{"Removed /etc/systemd/system/beat-agent.service", "Retained /etc/beat-agent", "--purge"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q: %s", want, output.String())
		}
	}
	if err := quietCommand(actions).Run(context.Background(), []string{"beat-agent", "service", "uninstall", "extra"}); err == nil {
		t.Fatal("expected positional argument error")
	}
	failing := quietCommand(actions)
	failing.Writer = failedWriter{}
	if err := failing.Run(context.Background(), []string{"beat-agent", "service", "uninstall"}); err == nil {
		t.Fatal("expected output error")
	}
	failure := errors.New("action failed")
	actions.uninstall = func(context.Context, agentinstall.UninstallOptions) (agentinstall.UninstallSummary, error) {
		return agentinstall.UninstallSummary{}, failure
	}
	if err := quietCommand(actions).Run(context.Background(), []string{"beat-agent", "service", "uninstall"}); !errors.Is(err, failure) {
		t.Fatalf("expected action error, got %v", err)
	}
}

func TestInstallAndJoinSurfaceActionAndOutputFailures(t *testing.T) {
	for _, mode := range []string{"join", "service"} {
		t.Run(mode, func(t *testing.T) {
			actions := testActions()
			failure := errors.New("action failed")
			actions.join = func(context.Context, agentinstall.JoinOptions) (agentinstall.Summary, error) {
				return agentinstall.Summary{}, failure
			}
			actions.install = func(context.Context, string) error { return failure }
			args := []string{"beat-agent", "service", "install"}
			if mode == "join" {
				args = []string{"beat-agent", "join", "--server", "https://beat.example.com", "--token", "token"}
			}
			if err := quietCommand(actions).Run(context.Background(), args); !errors.Is(err, failure) {
				t.Fatalf("expected action error, got %v", err)
			}
			command := quietCommand(testActions())
			command.Writer = failedWriter{}
			if err := command.Run(context.Background(), args); err == nil {
				t.Fatal("expected output error")
			}
		})
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
