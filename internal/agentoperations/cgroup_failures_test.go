//go:build linux

package agentoperations

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestContainmentAdmissionFailuresCleanOnlyOwnedGroups(t *testing.T) {
	for _, failure := range []string{"controller", "process", "guardian"} {
		t.Run(failure, func(t *testing.T) {
			config := strongTestConfig(t)
			settings, err := prepareSettings(config, "")
			if err != nil {
				t.Fatal(err)
			}
			engine, err := newEngine(settings)
			if err != nil {
				t.Fatal(err)
			}
			pid := math.MaxInt32
			if failure == "controller" {
				if err := writeControl(filepath.Join(config.Commands.CgroupRoot, "cgroup.subtree_control"), "-memory"); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "guardian" {
				child := exec.Command("/bin/sleep", "60")
				child.Env = []string{"PATH=/usr/bin:/bin"}
				child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
				pid = child.Process.Pid
				engine.executable = "/beat-missing-guardian-fixture"
			}
			if boundary, err := engine.attachBoundary("fault", pid); err == nil {
				_ = boundary.Close()
				t.Fatal("incomplete boundary accepted")
			}
			entries, err := os.ReadDir(config.Commands.CgroupRoot)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					t.Fatal("failed admission leaked a cgroup")
				}
			}
		})
	}
}

func TestCgroupDescriptorAndRecoveryErrorsFailClosed(t *testing.T) {
	if err := checkCgroupRoot("relative"); !errors.Is(err, ErrBoundary) {
		t.Fatal("relative cgroup accepted")
	}
	if err := checkCgroupRoot("/beat-no-such-cgroup"); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing cgroup accepted")
	}
	boundary := &linuxBoundary{directory: privateTestDir(t)}
	if err := boundary.guard("/missing"); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing kill descriptor accepted")
	}
	if err := removeEmptyCgroup(boundary.directory); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing population status accepted")
	}
	config := strongTestConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Recover("never-created"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.Commands.CgroupRoot, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := checkCgroupRoot(config.Commands.CgroupRoot); err == nil {
		t.Fatal("writable cgroup root accepted")
	}
}

func TestProcessStartupFailureCannotReleaseTheExecutionGate(t *testing.T) {
	config := testConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	engine.executable = "/beat-no-such-helper-fixture"
	running := false
	result, err := engine.Run(t.Context(), execution{lease: testLease("touch forbidden"), directory: privateTestDir(t), running: func() error { running = true; return nil }})
	if !errors.Is(err, ErrBoundary) || running || result.State == "succeeded" {
		t.Fatal("missing helper crossed durable execution gate")
	}
	if _, err := os.Stat(filepath.Join(settings.working, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("missing helper executed a payload")
	}
}
