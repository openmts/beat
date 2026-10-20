//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestMain(main *testing.M) {
	if handled, code := HandleHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(main.Run())
}

func privateTestDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testConfig(t *testing.T) Config {
	t.Helper()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := privateTestDir(t)
	working := filepath.Join(root, "work")
	if err := os.Mkdir(working, 0o700); err != nil {
		t.Fatal(err)
	}
	weaker := false
	return Config{Enabled: true, StateDir: filepath.Join(root, "state"), MaxConcurrency: 1, Commands: CommandConfig{Enabled: true, AllowAdHoc: true, RunAsUser: account.Username, AllowRoot: os.Geteuid() == 0, WorkingRoot: "work", MaximumTimeout: "5s", MaximumOutputBytes: 4096, RequireCgroup: &weaker}, FileRoots: []FileRoot{{ID: "work", Path: working, MaximumBytes: 4096}}}
}

func testLease(command string) protocol.Lease {
	return protocol.Lease{AttemptID: "attempt-1", Nonce: strings.Repeat("ab", 32), ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Payload: protocol.TaskInput{ShellFamily: "sh", Command: []byte(command), Limits: protocol.Limits{Kind: "shell", Targets: 1, TimeoutSeconds: 5, OutputBytes: 4096, RunAsRoot: os.Geteuid() == 0}}}
}

func executeTest(t *testing.T, command string) (protocol.Result, string) {
	t.Helper()
	config := testConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Run(t.Context(), execution{lease: testLease(command), directory: privateTestDir(t), running: func() error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return result, settings.working
}

func TestRestrictedShellUsesCleanEnvironmentAndNoNewPrivileges(t *testing.T) {
	t.Setenv("AGENT_TOKEN", "must-not-be-inherited")
	result, _ := executeTest(t, `printf '%s\n' "${AGENT_TOKEN-unset}"; id -u; sed -n '/^NoNewPrivs:/p' /proc/self/status; printf 'separate error' >&2`)
	if result.State != "succeeded" || !strings.Contains(string(result.Stdout), "unset") || !strings.Contains(string(result.Stdout), "NoNewPrivs:\t1") || string(result.Stderr) != "separate error" {
		t.Fatalf("unexpected execution: state=%s output=%q error=%q", result.State, result.Stdout, result.Stderr)
	}
	if err := result.Validate(4096); err != nil {
		t.Fatal(err)
	}
}

func TestOutputLimitAndExitStatus(t *testing.T) {
	result, _ := executeTest(t, "yes output")
	if result.State != "output_limit" || !result.Truncated || len(result.Stdout)+len(result.Stderr) > 4096 {
		t.Fatalf("output unbounded: %+v", result)
	}
	result, _ = executeTest(t, "exit 7")
	if result.State != "failed" || result.ExitCode != 7 {
		t.Fatalf("exit status: %+v", result)
	}
}

func TestTimeoutStopsDescendantProcessGroup(t *testing.T) {
	config := testConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeoutCause(t.Context(), 300*time.Millisecond, errTimeout)
	defer cancel()
	result, err := engine.Run(ctx, execution{lease: testLease("sleep 60 & echo $! > child.pid; wait"), directory: privateTestDir(t), running: func() error { return nil }})
	if err != nil || result.State != "timed_out" {
		t.Fatalf("timeout: %s %v", result.State, err)
	}
	content, err := os.ReadFile(filepath.Join(settings.working, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatal(err)
	}
	observation, stopObservation := context.WithTimeout(t.Context(), descendantObservationTimeout)
	defer stopObservation()
	if err := waitDescendantStopped(observation, pid); err != nil {
		t.Fatal(err)
	}
}

func TestDurableRunningFailureNeverExecutesScript(t *testing.T) {
	config := testConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Run(t.Context(), execution{lease: testLease("touch unauthorized"), directory: privateTestDir(t), running: func() error { return ErrStorage }})
	if !errors.Is(err, ErrStorage) {
		t.Fatal("storage failure was not returned")
	}
	if result.State != "interrupted" {
		t.Fatal("storage failure reported success")
	}
	if _, err := os.Stat(filepath.Join(settings.working, "unauthorized")); !os.IsNotExist(err) {
		t.Fatal("script executed before running journal")
	}
}

func TestCgroupRequirementFailsClosedOnOrdinaryDirectory(t *testing.T) {
	config := testConfig(t)
	config.Commands.RequireCgroup = nil
	config.Commands.CgroupRoot = privateTestDir(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newEngine(settings); err == nil {
		t.Fatal("ordinary directory accepted as strong containment")
	}
}

func TestLifetimeGuardianKillsOnParentPipeClosure(t *testing.T) {
	path := filepath.Join(privateTestDir(t), "kill-control")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, guardianArgument)
	command.ExtraFiles = []*os.File{read, file}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	_ = write.Close()
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "1" {
		t.Fatal("guardian did not request cgroup termination")
	}
}

func TestShellLosingExecutePermissionFailsWithoutRunningPayload(t *testing.T) {
	config := testConfig(t)
	shell := filepath.Join(privateTestDir(t), "shell")
	binary, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	config.Commands.ShellPath = shell
	config.Commands.MemoryBytes = 4 << 30
	config.Commands.MaximumOutputBytes = protocol.MaximumOutputBytes
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shell, 0o600); err != nil {
		t.Fatal(err)
	}
	lease := testLease("touch should-not-run")
	lease.Payload.Limits.OutputBytes = protocol.MaximumOutputBytes
	result, err := engine.Run(t.Context(), execution{lease: lease, directory: privateTestDir(t), running: func() error { return nil }})
	if err != nil || result.State != "failed" || result.ExitCode != helperFailure {
		t.Fatalf("invalid shell result: %s %d %v", result.State, result.ExitCode, err)
	}
	if _, err := os.Stat(filepath.Join(settings.working, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("payload ran with rejected shell")
	}
}

func TestHelperRejectsPostValidationFilesystemAndDescriptorFailures(t *testing.T) {
	for _, failure := range []string{"shell", "working", "limits", "gate"} {
		t.Run(failure, func(t *testing.T) {
			config := testConfig(t)
			config.Commands.MemoryBytes = 4 << 30
			config.Commands.MaximumOutputBytes = protocol.MaximumOutputBytes
			settings, err := prepareSettings(config, "")
			if err != nil {
				t.Fatal(err)
			}
			engine, err := newEngine(settings)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "shell":
				engine.settings.config.Commands.ShellPath = filepath.Join(privateTestDir(t), "removed-shell")
			case "working":
				engine.settings.working = filepath.Join(privateTestDir(t), "removed-working-root")
			case "limits":
				engine.settings.config.Commands.MemoryBytes = 1
			}
			lease := testLease("touch forbidden")
			lease.Payload.Limits.OutputBytes = protocol.MaximumOutputBytes
			launch, err := engine.prepareLaunch(execution{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			defer launch.close()
			if directory := os.Getenv("GOCOVERDIR"); directory != "" {
				launch.command.Env = append(launch.command.Env, "GOCOVERDIR="+directory)
			}
			if err := launch.command.Start(); err != nil {
				t.Fatal(err)
			}
			authorization := byte(1)
			if failure == "gate" {
				authorization = 0
			}
			if _, err := launch.gateWriter.Write([]byte{authorization}); err != nil {
				t.Fatal(err)
			}
			if err := launch.command.Wait(); err == nil {
				t.Fatal("corrupted helper admission accepted")
			}
		})
	}
}
