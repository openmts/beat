package main

import (
	"strings"
	"testing"
)

func TestCommandExitReportsUsageErrors(t *testing.T) {
	var output, failures strings.Builder
	if code := commandExit([]string{"beat-cli", "bogus"}, &output, &failures); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(failures.String(), "beat-cli: unknown command: bogus") {
		t.Fatalf("expected an actionable error, got %q", failures.String())
	}
}

func TestCommandExitStaysQuietWithoutArguments(t *testing.T) {
	var output, failures strings.Builder
	if code := commandExit(nil, &output, &failures); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(failures.String(), "a command is required") {
		t.Fatalf("expected a usage error, got %q", failures.String())
	}
}

func TestCommandExitPrintsHelp(t *testing.T) {
	var output, failures strings.Builder
	if code := commandExit([]string{"beat-cli", "--help"}, &output, &failures); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	for _, want := range []string{"health", "backups", "audit", "--token-file"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("expected help to mention %q, got %q", want, output.String())
		}
	}
}
