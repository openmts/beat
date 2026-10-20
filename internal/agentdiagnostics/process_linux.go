//go:build linux

package agentdiagnostics

import (
	"errors"
	"os/exec"
	"syscall"
)

func platformSupported() bool { return true }

// prepareProcess 把子进程放进独立进程组，取消时整组终止。
func prepareProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
