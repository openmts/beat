package agentdiagnostics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/beat/backend/internal/model"
)

// commandSpec 只描述已批准的绝对二进制、固定参数与输出上界。
type commandSpec struct {
	path         string
	arguments    []string
	directory    string
	maximumBytes int
}

// runError 表示子进程未能正常完成；调用方据此决定固定错误码。
type runError struct {
	kind string
}

func (err runError) Error() string { return err.kind }

// runFixedCommand 在私有工作目录中执行固定命令，清空环境并绑定进程组。
func runFixedCommand(ctx context.Context, spec commandSpec) ([]byte, error) {
	if !platformSupported() || !validBinaryPath(spec.path) {
		return nil, runError{kind: "unsupported"}
	}
	deadline, cancel := context.WithTimeout(ctx, maximumRunDuration())
	defer cancel()
	command := exec.CommandContext(deadline, spec.path, spec.arguments...)
	command.Dir = spec.directory
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C"}
	prepareProcess(command)
	var output boundedBuffer
	output.maximum = spec.maximumBytes
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, runError{kind: "start_failed"}
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-deadline.Done():
			// 取消时必须先杀整个进程组：只终止直接子进程会让孙进程继续持有
			// 输出管道，Wait 会一直等到孙进程自然退出。
			terminateProcessGroup(command)
		case <-finished:
		}
	}()
	waitErr := command.Wait()
	if deadline.Err() != nil {
		terminateProcessGroup(command)
		return nil, runError{kind: "timeout"}
	}
	if output.truncated {
		return nil, runError{kind: "output_limit"}
	}
	if waitErr != nil {
		return nil, runError{kind: "exit_nonzero"}
	}
	return output.Bytes(), nil
}

// terminateProcessGroup 忽略已证明终止的竞态失败，其余错误留给运维排查。
func terminateProcessGroup(command *exec.Cmd) {
	if err := killProcessTree(command); err != nil {
		_ = errors.Is(err, os.ErrProcessDone)
	}
}

func maximumRunDuration() time.Duration {
	return time.Duration(model.HardMaximumDiagnosticDurationSeconds+60) * time.Second
}

// classifyRunError 把子进程错误映射成固定状态与错误码。
func classifyRunError(ctx context.Context, err error) (outcome, error) {
	if ctx.Err() != nil {
		return terminalOutcome(model.DiagnosticStateCancelled, "canceled"), nil
	}
	var failure runError
	if errors.As(err, &failure) {
		switch failure.kind {
		case "timeout":
			return terminalOutcome(model.DiagnosticStateTimedOut, "timeout"), nil
		case "output_limit":
			return terminalOutcome(model.DiagnosticStateFailed, "output_limit"), nil
		case "unsupported":
			return terminalOutcome(model.DiagnosticStateFailed, "unsupported_platform"), nil
		case "start_failed":
			return terminalOutcome(model.DiagnosticStateFailed, "start_failed"), nil
		default:
			return terminalOutcome(model.DiagnosticStateFailed, "exit_nonzero"), nil
		}
	}
	if errors.Is(err, context.Canceled) {
		return terminalOutcome(model.DiagnosticStateCancelled, "canceled"), nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return terminalOutcome(model.DiagnosticStateTimedOut, "timeout"), nil
	}
	return terminalOutcome(model.DiagnosticStateFailed, "internal"), nil
}

// boundedBuffer 只保留前 maximum 字节，并记录是否被截断。
type boundedBuffer struct {
	buffer    bytes.Buffer
	maximum   int
	truncated bool
}

func (target *boundedBuffer) Write(content []byte) (int, error) {
	length := len(content)
	remaining := target.maximum - target.buffer.Len()
	if remaining <= 0 {
		target.truncated = true
		return length, nil
	}
	if length > remaining {
		target.truncated = true
		content = content[:remaining]
	}
	if _, err := target.buffer.Write(content); err != nil {
		return 0, err
	}
	return length, nil
}

func (target *boundedBuffer) Bytes() []byte { return target.buffer.Bytes() }
