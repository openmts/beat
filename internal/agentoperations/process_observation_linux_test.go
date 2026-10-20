//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const descendantObservationTimeout = 2 * time.Second

const descendantObservationInterval = 5 * time.Millisecond

// SIGKILL是异步操作；Wait只回收直接子进程，后代可能短暂处于X而不是Z。
// 此处只观察，不再发信号；真实存活的sleep不能因观察器自身而被终止。
func waitDescendantStopped(ctx context.Context, pid int) error {
	if pid <= 0 {
		return errors.New("invalid descendant PID")
	}
	ticker := time.NewTicker(descendantObservationInterval)
	defer ticker.Stop()
	for {
		stat, readErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		stopped, err := descendantStopped(stat, readErr)
		if err != nil {
			return fmt.Errorf("read descendant %d state: %w", pid, err)
		}
		if stopped {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("descendant %d remained active, stat=%q: %w", pid, stat, ctx.Err())
		case <-ticker.C:
		}
	}
}

func descendantStopped(stat []byte, readErr error) (bool, error) {
	if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, syscall.ESRCH) {
		return true, nil
	}
	if readErr != nil {
		return false, readErr
	}
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return false, errors.New("invalid descendant stat")
	}
	fields := strings.Fields(string(stat[end+2:]))
	if len(fields) == 0 || len(fields[0]) != 1 {
		return false, errors.New("missing descendant process state")
	}
	return fields[0] == "Z" || fields[0] == "X", nil
}

func TestDescendantObservationRecognizesOnlyTerminalStates(t *testing.T) {
	for _, state := range []string{"R", "S", "D", "T", "t", "I", "Z", "X"} {
		stopped, err := descendantStopped([]byte("123 (name with ) parens) "+state+" 1 123"), nil)
		expected := state == "Z" || state == "X"
		if err != nil || stopped != expected {
			t.Fatal(state, stopped, err)
		}
	}
	for _, missing := range []error{os.ErrNotExist, syscall.ESRCH} {
		if stopped, err := descendantStopped(nil, missing); err != nil || !stopped {
			t.Fatal(stopped, err)
		}
	}
	if stopped, err := descendantStopped(nil, os.ErrPermission); !errors.Is(err, os.ErrPermission) || stopped {
		t.Fatal(stopped, err)
	}
	for _, stat := range []string{"invalid", "123 (sleep) ", "123 (sleep) invalid"} {
		if stopped, err := descendantStopped([]byte(stat), nil); err == nil || stopped {
			t.Fatal(stat, stopped, err)
		}
	}
	if err := waitDescendantStopped(t.Context(), 0); err == nil {
		t.Fatal("invalid PID accepted")
	}
}

func TestDescendantObservationRejectsLiveProcessWithinBound(t *testing.T) {
	command := exec.Command("/bin/sleep", "60")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	})
	observation, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := waitDescendantStopped(observation, command.Process.Pid); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("live process accepted", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	stopped, cancelStopped := context.WithTimeout(t.Context(), descendantObservationTimeout)
	defer cancelStopped()
	if err := waitDescendantStopped(stopped, command.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed process exited successfully")
	}
	if err := waitDescendantStopped(stopped, command.Process.Pid); err != nil {
		t.Fatal("reaped process not recognized", err)
	}
}
