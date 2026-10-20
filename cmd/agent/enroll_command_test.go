package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentinstall"
)

func enrollActions(received *agentinstall.GrantOptions) agentActions {
	actions := testActions()
	actions.enroll = func(_ context.Context, options agentinstall.GrantOptions) (agentinstall.Summary, error) {
		*received = options
		return agentinstall.Summary{NodeID: "node-id", ServerURL: options.ServerURL,
			ConfigPath: agentinstall.DefaultConfigPath}, nil
	}
	return actions
}

func TestEnrollCommandReadsGrantFromStdin(t *testing.T) {
	var received agentinstall.GrantOptions
	command := quietCommand(enrollActions(&received))
	var output bytes.Buffer
	command.Writer, command.Reader = &output, strings.NewReader("beat_enroll_v1_secret\n")
	args := []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin",
		"--name", "node-a", "--wait", "45s"}
	if err := command.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if received.GrantSecret != "beat_enroll_v1_secret" || received.PollTimeout != 45*time.Second {
		t.Fatalf("unexpected options: %+v", received)
	}
	if strings.Contains(output.String(), "beat_enroll_v1_secret") {
		t.Fatalf("grant secret leaked into output: %s", output.String())
	}
}

func TestEnrollCommandReadsGrantFromPrivateFile(t *testing.T) {
	var received agentinstall.GrantOptions
	command := quietCommand(enrollActions(&received))
	path := filepath.Join(t.TempDir(), "grant.txt")
	if err := os.WriteFile(path, []byte("beat_enroll_v1_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-file", path}
	if err := command.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if received.GrantSecret != "beat_enroll_v1_file" {
		t.Fatalf("unexpected grant secret: %q", received.GrantSecret)
	}
}

func TestEnrollCommandRejectsLooseGrantFileAndMissingSource(t *testing.T) {
	var received agentinstall.GrantOptions
	path := filepath.Join(t.TempDir(), "grant.txt")
	if err := os.WriteFile(path, []byte("beat_enroll_v1_file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		{"beat-agent", "enroll", "--server", "https://beat.example.com"},
		{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-file", path},
		{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin", "--grant-file", path},
	}
	for _, args := range cases {
		command := quietCommand(enrollActions(&received))
		command.Reader = strings.NewReader("beat_enroll_v1_secret\n")
		if err := command.Run(context.Background(), args); err == nil {
			t.Fatalf("expected failure for %v", args)
		}
	}
}
