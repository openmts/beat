//go:build windows

package agentoperations

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultShell() string { return windowsDefaultShell }

func shellSupported(family string) bool { return family == "powershell" }

func resolveIdentity(config CommandConfig) (identity, error) {
	current, err := user.Current()
	if err != nil {
		return identity{}, ErrPolicy
	}
	if !strings.EqualFold(current.Username, config.RunAsUser) && !strings.EqualFold(current.Uid, config.RunAsUser) {
		return identity{}, ErrPolicy
	}
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if elevated && !config.AllowRoot {
		return identity{}, ErrPolicy
	}
	uid := uint32(1)
	if elevated {
		uid = 0
	}
	return identity{uid: uid, name: current.Username}, nil
}

func validateBoundary(settings settings) error {
	if !filepath.IsAbs(settings.config.Commands.ShellPath) {
		return ErrBoundary
	}
	info, err := os.Stat(settings.config.Commands.ShellPath)
	if err != nil || !info.Mode().IsRegular() {
		return ErrBoundary
	}
	if err := ensureCommandRootAccess(settings.working); err != nil {
		return err
	}
	token, err := restrictedCommandToken()
	if err != nil {
		return err
	}
	if err := token.Close(); err != nil {
		return err
	}
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return ErrBoundary
	}
	return windows.CloseHandle(handle)
}

func HandleHelper([]string) (bool, int) { return false, 0 }

func (engine *engine) Recover(id string) error {
	path := filepath.Join(engine.settings.config.StateDir, journalName(id), "command.ps1")
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type windowsBoundary struct{ job, process windows.Handle }

func (boundary *windowsBoundary) Kill() error { return windows.TerminateJobObject(boundary.job, 1) }

func (boundary *windowsBoundary) Close() error {
	return errors.Join(windows.CloseHandle(boundary.process), windows.CloseHandle(boundary.job))
}

func (engine *engine) execute(ctx context.Context, input execution, capture *outputCapture) (int, error) {
	script := filepath.Join(input.directory, "command.ps1")
	content := append([]byte{0xef, 0xbb, 0xbf}, input.lease.Payload.Command...)
	if err := os.WriteFile(script, content, 0o600); err != nil {
		return -1, ErrStorage
	}
	if err := secureCommandScript(script); err != nil {
		return -1, err
	}
	defer func() { _ = os.Remove(script) }()
	command := exec.Command(engine.settings.config.Commands.ShellPath, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	command.Env = []string{`PATH=C:\Windows\System32;C:\Windows`, `SystemRoot=C:\Windows`, "TEMP=" + engine.settings.working, "TMP=" + engine.settings.working, "USERPROFILE=" + engine.settings.working}
	command.Dir = engine.settings.working
	token, err := restrictedCommandToken()
	if err != nil {
		return -1, err
	}
	defer func() { _ = token.Close() }()
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW, Token: syscall.Token(token)}
	pipes, err := newOutputPipes(command, capture)
	if err != nil {
		return -1, err
	}
	defer pipes.finish()
	if err := command.Start(); err != nil {
		return -1, ErrBoundary
	}
	pipes.closeWriters()
	boundary, err := engine.assignJob(command.Process.Pid, input.lease.Payload.Limits.TimeoutSeconds)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return -1, err
	}
	defer func() { _ = boundary.Close() }()
	if err := input.running(); err != nil {
		_ = boundary.Kill()
		_ = command.Wait()
		return -1, ErrStorage
	}
	if err := resumeProcess(boundary.process); err != nil {
		_ = boundary.Kill()
		_ = command.Wait()
		return -1, err
	}
	return waitWindowsCommand(ctx, command, boundary)
}

func (engine *engine) assignJob(pid, timeout int) (*windowsBoundary, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, ErrBoundary
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME|windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, ErrBoundary
	}
	boundary := &windowsBoundary{job: job, process: process}
	limits := jobLimits(engine.settings.config.Commands, timeout)
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		_ = boundary.Close()
		return nil, ErrBoundary
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = boundary.Close()
		return nil, ErrBoundary
	}
	return boundary, nil
}

func jobLimits(config CommandConfig, timeout int) windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
	return windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_JOB_MEMORY | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_PROCESS_TIME | windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION, ActiveProcessLimit: uint32(config.MaximumProcesses), PerProcessUserTimeLimit: int64(timeout) * 10000000}, ProcessMemoryLimit: uintptr(config.MemoryBytes), JobMemoryLimit: uintptr(config.MemoryBytes)}
}

func resumeProcess(process windows.Handle) error {
	procedure := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
	if err := procedure.Find(); err != nil {
		return ErrBoundary
	}
	status, _, _ := procedure.Call(uintptr(process))
	if status != 0 {
		return ErrBoundary
	}
	return nil
}

func waitWindowsCommand(ctx context.Context, command *exec.Cmd, boundary *windowsBoundary) (int, error) {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		_ = boundary.Kill()
		waitErr = <-done
	}
	killErr := boundary.Kill()
	return command.ProcessState.ExitCode(), errors.Join(waitErr, killErr)
}

func supportedShells() []string { return []string{"powershell"} }

func operationPlatformSupported() bool { return true }
