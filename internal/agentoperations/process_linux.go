//go:build linux

package agentoperations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func defaultShell() string { return "/bin/sh" }

func resolveIdentity(config CommandConfig) (identity, error) {
	if os.Geteuid() != 0 {
		return identity{}, ErrBoundary
	}
	account, err := user.Lookup(config.RunAsUser)
	if err != nil {
		return identity{}, ErrPolicy
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return identity{}, ErrPolicy
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return identity{}, ErrPolicy
	}
	if uid == 0 && !config.AllowRoot {
		return identity{}, ErrPolicy
	}
	return identity{uint32(uid), uint32(gid), account.Username}, nil
}

func requireCgroup(config CommandConfig) bool {
	return config.RequireCgroup == nil || *config.RequireCgroup
}

func validateBoundary(settings settings) error {
	command := settings.config.Commands
	if !filepath.IsAbs(command.ShellPath) {
		return ErrBoundary
	}
	info, err := os.Stat(command.ShellPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ErrBoundary
	}
	if requireCgroup(command) {
		return checkCgroupRoot(command.CgroupRoot)
	}
	return nil
}

type helperConfig struct {
	Shell, Working         string
	UID, GID               uint32
	TimeoutSeconds         int
	OutputBytes            int
	MemoryBytes, Processes uint64
}

type launch struct {
	command                              *exec.Cmd
	configReader, gateReader, gateWriter *os.File
}

func (engine *engine) prepareLaunch(input execution) (*launch, error) {
	settings := engine.settings
	config := helperConfig{Shell: settings.config.Commands.ShellPath, Working: settings.working, UID: settings.identity.uid, GID: settings.identity.gid, TimeoutSeconds: input.lease.Payload.Limits.TimeoutSeconds, OutputBytes: input.lease.Payload.Limits.OutputBytes, MemoryBytes: settings.config.Commands.MemoryBytes, Processes: settings.config.Commands.MaximumProcesses}
	content, err := json.Marshal(config)
	if err != nil {
		return nil, ErrBoundary
	}
	configReader, configWriter, err := os.Pipe()
	if err != nil {
		return nil, ErrBoundary
	}
	_, writeErr := configWriter.Write(content)
	closeErr := configWriter.Close()
	if errors.Join(writeErr, closeErr) != nil {
		_ = configReader.Close()
		return nil, ErrBoundary
	}
	gateReader, gateWriter, err := os.Pipe()
	if err != nil {
		_ = configReader.Close()
		return nil, ErrBoundary
	}
	command := exec.Command(engine.executable, helperArgument)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	command.Stdin = bytes.NewReader(input.lease.Payload.Command)
	command.ExtraFiles = []*os.File{configReader, gateReader}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return &launch{command, configReader, gateReader, gateWriter}, nil
}

func (launch *launch) close() {
	_ = launch.configReader.Close()
	_ = launch.gateReader.Close()
	_ = launch.gateWriter.Close()
}

func (engine *engine) execute(ctx context.Context, input execution, capture *outputCapture) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	launch, err := engine.prepareLaunch(input)
	if err != nil {
		return -1, err
	}
	defer launch.close()
	pipes, err := newOutputPipes(launch.command, capture)
	if err != nil {
		return -1, err
	}
	defer pipes.finish()
	if err := launch.command.Start(); err != nil {
		return -1, ErrBoundary
	}
	pipes.closeWriters()
	boundary, err := engine.attachBoundary(input.lease.AttemptID, launch.command.Process.Pid)
	if err != nil {
		_ = killGroup(launch.command.Process.Pid)
		_ = launch.command.Wait()
		return -1, err
	}
	defer func() { _ = boundary.Close() }()
	if err := input.running(); err != nil {
		_ = boundary.Kill()
		_ = launch.command.Wait()
		return -1, ErrStorage
	}
	if _, err := launch.gateWriter.Write([]byte{1}); err != nil {
		_ = boundary.Kill()
		_ = launch.command.Wait()
		return -1, ErrBoundary
	}
	return waitCommand(ctx, launch.command, boundary)
}

type processBoundary interface {
	Kill() error
	Close() error
}

func waitCommand(ctx context.Context, command *exec.Cmd, boundary processBoundary) (int, error) {
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
	if killErr != nil {
		return command.ProcessState.ExitCode(), errors.Join(ErrBoundary, waitErr, killErr)
	}
	return command.ProcessState.ExitCode(), waitErr
}

func killGroup(pid int) error {
	err := unix.Kill(-pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func shellSupported(family string) bool { return family == "" || family == "sh" }

func supportedShells() []string { return []string{"sh"} }

func operationPlatformSupported() bool { return true }
