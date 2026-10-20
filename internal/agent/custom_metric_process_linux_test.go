package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCustomMetricStopHandlesCommandNotStarted(t *testing.T) {
	if err := stopCustomMetricCommand(&exec.Cmd{}); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("not-started process cleanup = %v", err)
	}
}

func customMetricTestQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func customMetricProcessRunning(pid int) bool {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(content), ')')
	return end >= 0 && end+2 < len(content) && content[end+2] != 'Z'
}

func waitCustomMetricChild(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitCustomCondition(t, func() bool {
		content, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		value, err := strconv.Atoi(strings.TrimSpace(string(content)))
		if err != nil || value <= 1 {
			return false
		}
		pid = value
		return true
	})
	t.Cleanup(func() {
		if customMetricProcessRunning(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return pid
}

func TestCustomMetricCancellationStopsDescendants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.pid")
	command := "umask 077; sleep 30 & printf '%s\n' \"$!\" > " + customMetricTestQuote(path) + "; wait"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		_, ok := CollectMetric(ctx, registeredMetric{Name: "child", ShellCommand: command})
		done <- ok
	}()
	pid := waitCustomMetricChild(t, path)
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("canceled script produced a sample")
		}
	case <-time.After(time.Second):
		t.Fatal("child inherited output pipe kept execution alive")
	}
	waitCustomCondition(t, func() bool { return !customMetricProcessRunning(pid) })
}

func TestCustomMetricExitStopsBackgroundDescendants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background.pid")
	command := "umask 077; sleep 30 >/dev/null 2>&1 & printf '%s\n' \"$!\" > " + customMetricTestQuote(path) + "; printf 7"
	value, ok := CollectMetric(t.Context(), registeredMetric{Name: "background", ShellCommand: command})
	if !ok || value != 7 {
		t.Fatal("completed sample failed")
	}
	pid := waitCustomMetricChild(t, path)
	waitCustomCondition(t, func() bool { return !customMetricProcessRunning(pid) })
}
