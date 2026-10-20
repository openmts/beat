//go:build !linux

package agentdiagnostics

import "os/exec"

// 只有 Linux 提供经过验收的进程组边界；其他平台不执行诊断。
func platformSupported() bool { return false }

func prepareProcess(*exec.Cmd) {}

func killProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
