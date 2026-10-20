//go:build linux

package agentoperations

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func isolatedCgroup(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/sys/fs/cgroup", "beat-agent-fixture-")
	if err != nil {
		t.Skipf("cgroup delegation unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(root); err != nil {
			t.Errorf("remove isolated cgroup: %v", err)
		}
	})
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeControl(filepath.Join(root, "cgroup.subtree_control"), "+cpu +memory +pids"); err != nil {
		t.Fatal(err)
	}
	return root
}

func strongTestConfig(t *testing.T) Config {
	config := testConfig(t)
	config.Commands.RequireCgroup = nil
	config.Commands.CgroupRoot = isolatedCgroup(t)
	return config
}

func TestCgroupKillsEscapedSessionDescendants(t *testing.T) {
	config := strongTestConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeoutCause(t.Context(), 350*time.Millisecond, errTimeout)
	defer cancel()
	result, err := engine.Run(ctx, execution{lease: testLease("setsid sleep 60 & echo $! > escaped.pid; wait"), directory: privateTestDir(t), running: func() error { return nil }})
	if err != nil || result.State != "timed_out" {
		t.Fatalf("strong containment: %s %v stderr=%s", result.State, err, result.Stderr)
	}
	assertTerminatedPID(t, filepath.Join(settings.working, "escaped.pid"))
	entries, err := os.ReadDir(config.Commands.CgroupRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("attempt cgroup leaked")
		}
	}
}

func TestCgroupGuardianSurvivesAgentCrash(t *testing.T) {
	config := strongTestConfig(t)
	content, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestAgentCrashChild$")
	child.Env = append(os.Environ(), "BEAT_TEST_AGENT_OPERATION_CHILD="+string(content))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(config.FileRoots[0].Path, "escaped.pid")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	assertTerminatedPID(t, marker)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Recover("attempt-1"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentCrashChild(t *testing.T) {
	value := os.Getenv("BEAT_TEST_AGENT_OPERATION_CHILD")
	if value == "" {
		return
	}
	var config Config
	if err := json.Unmarshal([]byte(value), &config); err != nil {
		t.Fatal(err)
	}
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(filepath.Dir(config.StateDir), "child-output")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = engine.Run(t.Context(), execution{lease: testLease("setsid sleep 60 & echo $! > escaped.pid; wait"), directory: directory, running: func() error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
}

func assertTerminatedPID(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) || (err == nil && strings.Contains(string(stat), ") Z ")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("escaped descendant survived boundary termination")
}
