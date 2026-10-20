package agentoperations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

var errOutputLimit = errors.New("output_limit")

var errTimeout = errors.New("timeout")

var errCanceled = errors.New("canceled")

var errDisconnected = errors.New("interrupted")

const helperArgument = "--beat-operation-helper"

const guardianArgument = "--beat-operation-guardian"

type identity struct {
	uid, gid uint32
	name     string
}

type execution struct {
	lease     protocol.Lease
	directory string
	running   func() error
}

type processEngine interface {
	Run(context.Context, execution) (protocol.Result, error)
	Recover(string) error
}

type engine struct {
	settings   settings
	executable string
}

type outputCapture struct {
	mu             sync.Mutex
	stdout, stderr *os.File
	remaining      int
	truncated      bool
	failure        error
	cancel         context.CancelCauseFunc
}

type outputWriter struct {
	capture *outputCapture
	file    *os.File
}

func (writer outputWriter) Write(content []byte) (int, error) {
	capture := writer.capture
	capture.mu.Lock()
	defer capture.mu.Unlock()
	accepted := min(len(content), capture.remaining)
	if accepted > 0 {
		written, err := writer.file.Write(content[:accepted])
		capture.remaining -= written
		if err != nil {
			capture.failure = ErrStorage
			capture.cancel(ErrStorage)
			return written, err
		}
	}
	if accepted < len(content) {
		capture.truncated = true
		capture.cancel(errOutputLimit)
	}
	return len(content), nil
}

func openCapture(directory string, limit int, cancel context.CancelCauseFunc) (*outputCapture, error) {
	stdout, err := os.OpenFile(filepath.Join(directory, "stdout"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrStorage
	}
	if err := secureCreatedFile(stdout); err != nil {
		_ = stdout.Close()
		return nil, ErrStorage
	}
	stderr, err := os.OpenFile(filepath.Join(directory, "stderr"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = stdout.Close()
		return nil, ErrStorage
	}
	if err := secureCreatedFile(stderr); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, ErrStorage
	}
	return &outputCapture{stdout: stdout, stderr: stderr, remaining: limit, cancel: cancel}, nil
}

func (capture *outputCapture) close() error {
	return errors.Join(capture.stdout.Sync(), capture.stderr.Sync(), capture.stdout.Close(), capture.stderr.Close(), capture.failure)
}

func newEngine(settings settings) (*engine, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, ErrBoundary
	}
	if settings.config.Enabled && settings.config.Commands.Enabled {
		if err := validateBoundary(settings); err != nil {
			return nil, err
		}
	}
	return &engine{settings: settings, executable: executable}, nil
}

func (engine *engine) Run(parent context.Context, input execution) (protocol.Result, error) {
	started := time.Now().UTC()
	if parent.Err() != nil {
		return terminalResult("interrupted", started), parent.Err()
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	capture, err := openCapture(input.directory, input.lease.Payload.Limits.OutputBytes, cancel)
	if err != nil {
		return terminalResult("interrupted", started), err
	}
	exitCode, runErr := engine.execute(ctx, input, capture)
	closeErr := capture.close()
	result := terminalResult(resultState(ctx, capture, runErr), started)
	result.ExitCode = exitCode
	result.Truncated = capture.truncated
	if err := readCaptured(&result, input.directory); err != nil {
		return result, err
	}
	if closeErr != nil || errors.Is(runErr, ErrStorage) {
		result.State = "interrupted"
		result.Reason = "storage_failure"
		return result, ErrStorage
	}
	if errors.Is(runErr, ErrBoundary) {
		result.Reason = "sandbox_unavailable"
		return result, ErrBoundary
	}
	return result, nil
}

func resultState(ctx context.Context, capture *outputCapture, runErr error) string {
	if capture.truncated {
		return "output_limit"
	}
	switch context.Cause(ctx) {
	case errTimeout:
		return "timed_out"
	case errCanceled:
		return "canceled"
	case nil:
		if runErr == nil {
			return "succeeded"
		}
		return "failed"
	default:
		return "interrupted"
	}
}

func terminalResult(state string, started time.Time) protocol.Result {
	reasons := map[string]string{"succeeded": "completed", "failed": "exit_nonzero", "timed_out": "timeout", "canceled": "canceled", "output_limit": "output_limit", "interrupted": "interrupted"}
	return protocol.Result{State: state, ExitCode: -1, Reason: reasons[state], StartedAt: started.UnixMilli(), FinishedAt: time.Now().UTC().UnixMilli()}
}

func readCaptured(result *protocol.Result, directory string) error {
	stdout, err := os.ReadFile(filepath.Join(directory, "stdout"))
	if err != nil {
		return ErrStorage
	}
	stderr, err := os.ReadFile(filepath.Join(directory, "stderr"))
	if err != nil {
		return ErrStorage
	}
	result.Stdout, result.Stderr = stdout, stderr
	result.StdoutBytes, result.StderrBytes = len(stdout), len(stderr)
	outDigest, errDigest := sha256.Sum256(stdout), sha256.Sum256(stderr)
	result.StdoutSHA256, result.StderrSHA256 = hex.EncodeToString(outDigest[:]), hex.EncodeToString(errDigest[:])
	return nil
}

type outputPipes struct {
	outRead, outWrite, errRead, errWrite *os.File
	done                                 sync.WaitGroup
}

func newOutputPipes(command *exec.Cmd, capture *outputCapture) (*outputPipes, error) {
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		_ = outRead.Close()
		_ = outWrite.Close()
		return nil, err
	}
	pipes := &outputPipes{outRead: outRead, outWrite: outWrite, errRead: errRead, errWrite: errWrite}
	command.Stdout, command.Stderr = outWrite, errWrite
	pipes.done.Add(2)
	go pipes.drain(outRead, outputWriter{capture, capture.stdout})
	go pipes.drain(errRead, outputWriter{capture, capture.stderr})
	return pipes, nil
}

func (pipes *outputPipes) drain(reader *os.File, writer outputWriter) {
	defer pipes.done.Done()
	if _, err := io.Copy(writer, reader); err != nil {
		writer.capture.cancel(ErrStorage)
	}
	_ = reader.Close()
}

func (pipes *outputPipes) closeWriters() { _ = pipes.outWrite.Close(); _ = pipes.errWrite.Close() }

func (pipes *outputPipes) finish() { pipes.closeWriters(); pipes.done.Wait() }
