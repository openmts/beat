//go:build linux

package acceleratorcollect

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixtureCommand(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vendor-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	original := collectorPaths
	collectorPaths = map[string]string{"nvidia": path, "amd": filepath.Join(t.TempDir(), "missing")}
	t.Cleanup(func() { collectorPaths = original })
	return path
}

func TestFixedCollectorUsesCleanEnvironmentAndBoundedProcess(t *testing.T) {
	t.Setenv("BEAT_GPU_FIXTURE_SECRET", "private")
	path := fixtureCommand(t, `printf '%s' "${BEAT_GPU_FIXTURE_SECRET-unset}"; printf 'PRIVATE_STDERR' >&2`)
	output, err := (commandExecutor{}).Run(t.Context(), "nvidia")
	if err != nil || string(output) != "unset" {
		t.Fatal(string(output), err)
	}
	if _, err = (commandExecutor{}).Run(t.Context(), "arbitrary"); err != ErrCollector {
		t.Fatal(err)
	}
	if _, err = (commandExecutor{}).Run(t.Context(), "amd"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = (commandExecutor{}).Run(t.Context(), "nvidia"); err != ErrCollector {
		t.Fatal(err)
	}
}

func TestCollectorTimeoutAndOutputLimitTerminateOnlyFixtureGroup(t *testing.T) {
	fixtureCommand(t, `sleep 5`)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := (commandExecutor{}).Run(ctx, "nvidia"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestCollectorRejectsFailedAndOversizedToolOutput(t *testing.T) {
	t.Run("exit", func(t *testing.T) {
		fixtureCommand(t, `printf 'PRIVATE_FAILURE' >&2; exit 7`)
		if _, err := (commandExecutor{}).Run(t.Context(), "nvidia"); err != ErrCollector {
			t.Fatal(err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		fixtureCommand(t, `head -c 262145 /dev/zero`)
		if _, err := (commandExecutor{}).Run(t.Context(), "nvidia"); err != ErrOutputLimit {
			t.Fatal(err)
		}
	})
}

func TestCombinedOutputCapAndOptionalPowerFailure(t *testing.T) {
	canceled := false
	output := &boundedOutput{capture: true, cancel: func() { canceled = true }}
	stderr := &boundedOutput{shared: output}
	if _, err := output.Write(bytes.Repeat([]byte{1}, MaximumOutputBytes-1)); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write([]byte{1, 2}); err != ErrOutputLimit || !canceled || !output.overflow {
		t.Fatal(err)
	}
	devices, err := ParseNVIDIA([]byte("0000:01:00.0, Fixture, 1, 0, 1, 2, 50, invalid, 100\n"))
	if err != nil || len(devices[0].Errors) != 1 {
		t.Fatal(devices, err)
	}
	if _, exists := devices[0].Metrics["gpu_power_watts"]; exists {
		t.Fatal("invalid optional metric persisted")
	}
}

func TestCollectorCleansDescendantAfterToolLeaderExits(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	fixtureCommand(t, "umask 077; sleep 60 & echo $! > '"+pidPath+"'; exit 0")
	if _, err := (commandExecutor{}).Run(t.Context(), "nvidia"); err != ErrCollector {
		t.Fatal("inherited output handle did not fail", err)
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		raw, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		end := strings.LastIndex(string(raw), ") ")
		if end >= 0 && (raw[end+2] == 'X' || raw[end+2] == 'Z') {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture descendant remained active: %s", raw)
		}
		time.Sleep(time.Millisecond)
	}
}
