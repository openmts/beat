package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const maxScriptOutputBytes = 64 << 10

type ScriptResult struct {
	Status     string
	Output     string
	ExitCode   int
	DurationMs int64
}

func ExecuteScript(ctx context.Context, script string, timeout time.Duration) ScriptResult {
	startedAt := time.Now()
	if timeout <= 0 || timeout > 5*time.Minute {
		timeout = 30 * time.Second
	}
	executionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output := &cappedBuffer{maxBytes: maxScriptOutputBytes}
	command := exec.Command("/bin/sh", "-c", script)
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return ScriptResult{Status: "failed", Output: err.Error(), ExitCode: -1, DurationMs: time.Since(startedAt).Milliseconds()}
	}

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	var commandErr error
	timedOut := false
	select {
	case commandErr = <-done:
	case <-executionCtx.Done():
		timedOut = errors.Is(executionCtx.Err(), context.DeadlineExceeded)
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		commandErr = <-done
	}

	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	status := "success"
	if timedOut {
		status = "timeout"
		if output.Len() == 0 {
			_, _ = output.Write([]byte("任务执行超时，进程已被终止。"))
		}
	} else if commandErr != nil {
		status = "failed"
	}
	return ScriptResult{
		Status:     status,
		Output:     output.String(),
		ExitCode:   exitCode,
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func (w *cappedBuffer) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	originalLength := len(value)
	remaining := w.maxBytes - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = true
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(value)
	return originalLength, nil
}

func (w *cappedBuffer) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Len()
}

func (w *cappedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	value := w.buffer.String()
	if w.truncated {
		value += "\n[output truncated]"
	}
	return value
}
