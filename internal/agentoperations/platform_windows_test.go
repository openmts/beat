//go:build windows

package agentoperations

import (
	"bytes"
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func windowsPrivateDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := secureCreatedDirectory(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeWindowsPrivate(t *testing.T, path string, content []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := secureCreatedFile(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func windowsExecution(t *testing.T, command string) (*engine, execution) {
	t.Helper()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := windowsPrivateDirectory(t)
	working := filepath.Join(root, "work")
	if err := createPrivateDirectory(working); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	if err := createPrivateDirectory(state); err != nil {
		t.Fatal(err)
	}
	config := Config{Enabled: true, StateDir: state, Commands: CommandConfig{Enabled: true, AllowAdHoc: true, RunAsUser: account.Username, AllowRoot: windows.GetCurrentProcessToken().IsElevated(), WorkingRoot: "work", MaximumTimeout: "10s", MaximumOutputBytes: 65536}, FileRoots: []FileRoot{{ID: "work", Path: working, MaximumBytes: 65536}}}
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(state, "attempt")
	if err := createPrivateDirectory(output); err != nil {
		t.Fatal(err)
	}
	lease := protocol.Lease{AttemptID: "windows-attempt", Nonce: strings.Repeat("ab", 32), ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Payload: protocol.TaskInput{ShellFamily: "powershell", Command: []byte(command), Limits: protocol.Limits{Kind: "shell", Targets: 1, TimeoutSeconds: 10, OutputBytes: 65536, RunAsRoot: settings.identity.uid == 0}}}
	return engine, execution{lease: lease, directory: output, running: func() error { return nil }}
}

func TestWindowsRestrictedTokenCannotReadOrRewritePrivateState(t *testing.T) {
	engine, input := windowsExecution(t, "placeholder")
	protected := filepath.Join(engine.settings.config.StateDir, "policy.json")
	writeWindowsPrivate(t, protected, []byte("private-policy"))
	escaped := strings.ReplaceAll(protected, "'", "''")
	command := "$ErrorActionPreference='Stop'; try { [IO.File]::ReadAllText('" + escaped + "'); exit 17 } catch {}; try { [IO.File]::WriteAllText('" + escaped + "','changed'); exit 18 } catch {}; [IO.File]::WriteAllText((Join-Path $PWD 'allowed.txt'),'allowed'); Write-Output 'restricted'"
	input.lease.Payload.Command = []byte(command)
	result, err := engine.Run(t.Context(), input)
	if err != nil || result.State != "succeeded" || !bytes.Contains(result.Stdout, []byte("restricted")) {
		t.Fatalf("restricted execution failed: state=%s code=%d err=%v", result.State, result.ExitCode, err)
	}
	actual, err := os.ReadFile(protected)
	if err != nil || string(actual) != "private-policy" {
		t.Fatal("command accessed protected state")
	}
	actual, err = os.ReadFile(filepath.Join(engine.settings.working, "allowed.txt"))
	if err != nil || string(actual) != "allowed" {
		t.Fatal("configured workspace was not writable")
	}
}

func TestWindowsJobTerminatesDescendantsOnTimeout(t *testing.T) {
	command := `$p=New-Object Diagnostics.ProcessStartInfo; $p.FileName='C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe'; $p.Arguments='-NoProfile -NonInteractive -Command Start-Sleep -Seconds 60'; $p.UseShellExecute=$false; $child=[Diagnostics.Process]::Start($p); [IO.File]::WriteAllText((Join-Path $PWD 'child.pid'),[string]$child.Id); Start-Sleep -Seconds 60`
	engine, input := windowsExecution(t, command)
	ctx, cancel := context.WithTimeoutCause(t.Context(), 3*time.Second, errTimeout)
	defer cancel()
	result, err := engine.Run(ctx, input)
	if err != nil || result.State != "timed_out" {
		t.Fatalf("job timeout failed: %s %v", result.State, err)
	}
	content, err := os.ReadFile(filepath.Join(engine.settings.working, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(process) }()
	state, err := windows.WaitForSingleObject(process, 2000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("descendant survived job termination")
	}
}

func TestWindowsHandlePublicationHonorsNoReplaceAndPrivateMode(t *testing.T) {
	directory := windowsPrivateDirectory(t)
	writeWindowsPrivate(t, filepath.Join(directory, "target"), []byte("original"))
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	destination, err := root.destination("target")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = destination.Close() }()
	name, err := temporaryName()
	if err != nil {
		t.Fatal(err)
	}
	file, err := destination.temporary(name, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("replacement")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := destination.publish(name, false); err == nil {
		t.Fatal("no-replace publication overwrote a file")
	}
	if err := destination.publish(name, true); err != nil {
		t.Fatal(err)
	}
	current, err := destination.current()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = current.Close() }()
	if !transferModeMatches(current, 0o600) {
		t.Fatal("non-executable ACL was not retained")
	}
	actual, err := os.ReadFile(filepath.Join(directory, "target"))
	if err != nil || string(actual) != "replacement" {
		t.Fatal("replacement was not atomic")
	}
}

func TestWindowsDefaultShellPath(t *testing.T) {
	if defaultShell() != windowsDefaultShell {
		t.Fatalf("unexpected PowerShell path: %q", defaultShell())
	}
}
